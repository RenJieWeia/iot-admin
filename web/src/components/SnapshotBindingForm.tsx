import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { ApiError, createRow, fetchList, fetchOne, updateRow } from '../api/client';
import type { FieldMeta, RowData } from '../api/types';
import { Drawer } from './Drawer';
import { FilterMultiSelect } from './FilterMultiSelect';
import { RelationSelect } from './RelationSelect';

const BINDINGS_ENTITY = 'snapshot-bindings';
const PROPERTIES_ENTITY = 'thing-model-properties';

/** 终端类型下拉：专用表单直接给 RelationSelect 造一个字段描述。 */
const TYPE_FIELD: FieldMeta = {
  key: 'device_type_id',
  label: '终端类型',
  type: 'relation',
  visible: true,
  sortable: false,
  filterable: false,
  filterType: 'relation',
  operators: ['in'],
  operatorLabels: {},
  optionsSource: 'remote',
  options: [],
  editable: true,
  required: true,
  widthStrategy: 'min',
  ref: { entity: 'device-types', valueKey: 'id', labelKey: 'name' },
};

type Scope = 'type' | 'terminal';

interface Props {
  open: boolean;
  onClose: () => void;
  onSaved: () => void;
  /** 编辑态：传入现有绑定行；不传即新增。 */
  editing?: RowData | null;
}

/** 兼容两种形态：新模型 `item_codes` 数组、旧模型单个 `item_code`。 */
function codesOf(row: RowData | null | undefined): string[] {
  const raw = row?.item_codes;
  if (Array.isArray(raw)) return raw.map((value) => String(value));
  if (typeof row?.item_code === 'string' && row.item_code !== '') return [row.item_code];
  return [];
}

/**
 * 绑定表单（新增与编辑共用）：单目标 + 多字段。
 *
 * 级联约束（两处清空都必须做）：
 *   1) 切作用域（类型 ↔ 终端）→ 清空「目标 + 已选字段」；
 *   2) 同作用域内换目标 → **只清空已选字段**（作用域保留）。
 * 字段候选来自目标类型的终端属性；选终端时先由终端行拿 type_id 再取候选。
 */
export function SnapshotBindingForm({ open, onClose, onSaved, editing = null }: Props) {
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<Scope>('type');
  const [typeID, setTypeID] = useState('');
  const [terminalID, setTerminalID] = useState('');
  const [terminalTypeID, setTerminalTypeID] = useState('');
  const [keyword, setKeyword] = useState('');
  const [debounced, setDebounced] = useState('');
  const [codes, setCodes] = useState<string[]>([]);
  const [formError, setFormError] = useState<string | null>(null);

  const isEdit = editing !== null;
  const editingID = editing ? String(editing.id ?? '') : '';

  // 打开时按编辑态初始化：目标与已选字段从行里回填；新增则清空。
  useEffect(() => {
    if (!open) return;
    setFormError(null);
    setKeyword('');
    setDebounced('');
    if (editing) {
      const isType = editing.device_type_id !== null && editing.device_type_id !== undefined;
      setScope(isType ? 'type' : 'terminal');
      setTypeID(isType ? String(editing.device_type_id) : '');
      setTerminalID(isType ? '' : String(editing.terminal_id ?? ''));
      setTerminalTypeID('');
      setCodes(codesOf(editing));
    } else {
      setScope('type');
      setTypeID('');
      setTerminalID('');
      setTerminalTypeID('');
      setCodes([]);
    }
  }, [open, editing]);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(keyword.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [keyword]);

  // 编辑实例级绑定时，行里只有 terminal_id：查一次终端拿 type_id，才能取字段候选。
  const terminalRow = useQuery({
    queryKey: ['bindings', 'terminal', terminalID],
    queryFn: ({ signal }) => fetchOne('terminals', terminalID, signal),
    enabled: open && scope === 'terminal' && terminalID !== '' && terminalTypeID === '',
  });
  const resolvedTerminalTypeID = terminalTypeID || String(terminalRow.data?.type_id ?? '');

  const terminals = useQuery({
    queryKey: ['bindings', 'terminals', debounced],
    queryFn: ({ signal }) => fetchList('terminals', { q: debounced, pageSize: 10 }, signal),
    enabled: open && scope === 'terminal' && debounced !== '',
    placeholderData: (prev) => prev,
  });

  const effectiveTypeID = scope === 'type' ? typeID : resolvedTerminalTypeID;

  const properties = useQuery({
    queryKey: ['bindings', 'properties', effectiveTypeID],
    queryFn: ({ signal }) =>
      fetchList(
        PROPERTIES_ENTITY,
        { filters: [{ field: 'type_id', op: 'in', values: [effectiveTypeID] }], pageSize: 50 },
        signal,
      ),
    enabled: open && effectiveTypeID !== '',
    placeholderData: (prev) => prev,
  });

  const fieldOptions = useMemo(
    () =>
      (properties.data?.rows ?? []).map((row) => ({
        value: String(row.item_code ?? ''),
        label: row.name ? `${String(row.item_code)}（${String(row.name)}）` : String(row.item_code ?? ''),
      })),
    [properties.data],
  );
  const labelOf = (code: string) => fieldOptions.find((option) => option.value === code)?.label ?? code;

  const save = useMutation({
    mutationFn: (body: RowData) =>
      isEdit ? updateRow(BINDINGS_ENTITY, editingID, body) : createRow(BINDINGS_ENTITY, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['list', BINDINGS_ENTITY] });
      onSaved();
      onClose();
    },
    onError: (error) => {
      // 目标重复、字段不属于该类型等 400：原因原样显示在表单内，抽屉不关。
      setFormError(error instanceof ApiError ? error.detail : String(error));
    },
  });

  const switchScope = (next: Scope) => {
    // 作用域变化：目标与已选字段都作废（候选集完全换了）。
    setScope(next);
    setTypeID('');
    setTerminalID('');
    setTerminalTypeID('');
    setKeyword('');
    setDebounced('');
    setCodes([]);
    setFormError(null);
  };

  const changeTarget = (nextTypeID: string, nextTerminalID = '', nextTerminalType = '') => {
    // 同作用域内换目标：只清空已选字段，作用域保留。
    setTypeID(nextTypeID);
    if (nextTerminalID !== '') {
      setTerminalID(nextTerminalID);
      setTerminalTypeID(nextTerminalType);
    }
    setCodes([]);
    setFormError(null);
  };

  const submit = () => {
    if (effectiveTypeID === '') {
      setFormError(scope === 'type' ? '请选择终端类型' : '请先选择终端');
      return;
    }
    setFormError(null);
    // 两列始终带齐，不属于当前作用域的一侧显式给 null。
    // 后端 update 会把库里旧值合并进校验视图，只传新作用域那一列会让两列同时非空 → 400。
    const body: RowData =
      scope === 'type'
        ? { device_type_id: typeID, terminal_id: null, item_codes: codes }
        : { device_type_id: null, terminal_id: terminalID, item_codes: codes };
    save.mutate(body);
  };

  const canSave = codes.length > 0 && !save.isPending;

  return (
    <Drawer
      open={open}
      title={isEdit ? '编辑快照绑定' : '新建快照绑定'}
      onClose={onClose}
      description="一条绑定 = 一个目标（终端类型或终端）+ 多个数据项；字段候选来自目标类型的终端属性。"
      footer={
        <>
          <span className="mr-auto text-sm text-muted">已选 {codes.length} 个字段</span>
          <button type="button" className="btn" onClick={onClose} disabled={save.isPending}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={submit}
            disabled={!canSave}
            aria-busy={save.isPending}
            title={codes.length === 0 ? '至少选择一个字段后才能保存' : undefined}
          >
            保存
          </button>
        </>
      }
    >
      {isEdit && (
        <p className="m-0 rounded-sm border border-warning bg-warning-weak px-3 py-2 text-warning" role="status">
          修改只影响后续写入，已产生的快照不变。
        </p>
      )}

      <fieldset className="field m-0 border-0 p-0">
        <legend className="mb-1 p-0 text-base">作用域</legend>
        <div className="flex gap-4">
          {(['type', 'terminal'] as const).map((value) => (
            <label key={value} className="inline-flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="binding-scope"
                value={value}
                checked={scope === value}
                onChange={() => switchScope(value)}
              />
              {value === 'type' ? '终端类型（整类）' : '终端（单个）'}
            </label>
          ))}
        </div>
      </fieldset>

      {scope === 'type' ? (
        <div className="field">
          <label htmlFor="binding-type">终端类型</label>
          <RelationSelect
            field={TYPE_FIELD}
            id="binding-type"
            value={typeID}
            onChange={(next) => changeTarget(next)}
            allowEmpty={false}
          />
        </div>
      ) : (
        <div className="field">
          <label htmlFor="binding-terminal">终端</label>
          <input
            id="binding-terminal"
            className="input"
            type="search"
            placeholder="按终端 ID 或名称搜索"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
          {terminalID !== '' && (
            <p className="m-0 text-sm text-muted">
              已选终端：{terminalID}
              {effectiveTypeID ? `（类型 ${effectiveTypeID}）` : ''}
            </p>
          )}
          {debounced !== '' && terminals.isFetching && (
            <p className="m-0 text-sm text-muted" role="status">正在搜索终端…</p>
          )}
          {debounced !== '' && !terminals.isFetching && (
            <ul className="m-0 flex list-none flex-col gap-1 p-0" aria-label="终端候选">
              {(terminals.data?.rows ?? []).map((row) => (
                <li key={String(row.id)}>
                  <button
                    type="button"
                    className="btn btn-link"
                    aria-pressed={String(row.id) === terminalID}
                    onClick={() => changeTarget('', String(row.id), String(row.type_id ?? ''))}
                  >
                    {String(row.id)}（{String(row.name ?? '未命名')}）
                  </button>
                </li>
              ))}
              {(terminals.data?.rows ?? []).length === 0 && (
                <li className="text-sm text-muted">没有匹配的终端</li>
              )}
            </ul>
          )}
        </div>
      )}

      <div className="field">
        <span className="text-base" id="binding-fields-label">字段</span>
        <FilterMultiSelect
          label="字段"
          options={fieldOptions}
          selected={codes}
          onChange={(next) => {
            setCodes(next);
            setFormError(null);
          }}
          allowSelectAll
        />
        {codes.length > 0 && (
          <div className="flex flex-wrap items-center gap-1" aria-label="已选字段">
            {codes.map((code) => (
              <button
                key={code}
                type="button"
                className="btn btn-link"
                aria-label={`移除字段 ${code}`}
                onClick={() => setCodes((prev) => prev.filter((item) => item !== code))}
              >
                {labelOf(code)} ✕
              </button>
            ))}
          </div>
        )}
        <p className="m-0 text-sm text-muted">
          {effectiveTypeID === ''
            ? '先选择目标，字段候选来自目标类型的终端属性。'
            : `候选来自终端类型 ${effectiveTypeID} 的终端属性；可搜索，支持全选/反选。`}
        </p>
        {codes.length === 0 && (
          <p className="m-0 text-sm text-warning">至少选择一个字段后才能保存。</p>
        )}
      </div>

      {formError && (
        <p className="m-0 rounded-sm border border-negative-text bg-negative-bg px-3 py-2 text-negative-text" role="alert">
          {formError}
        </p>
      )}
    </Drawer>
  );
}

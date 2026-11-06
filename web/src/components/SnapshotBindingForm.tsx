import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { ApiError, createRow, fetchList } from '../api/client';
import type { FieldMeta, RowData } from '../api/types';
import { Drawer } from './Drawer';
import { RelationSelect } from './RelationSelect';

const BINDINGS_ENTITY = 'snapshot-bindings';
const PROPERTIES_ENTITY = 'thing-model-properties';

/** 终端类型下拉：这是一个专用表单，不需要通用字段元数据，直接给 RelationSelect 造一个字段描述。 */
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
  onCreated: () => void;
}

/**
 * 绑定专用表单：作用域（类型级 / 实例级）→ 目标 → 数据项。
 *
 * 级联顺序是硬约束：
 *   类型级：选类型 → 用该类型过滤 `thing-model-properties` 取 item_code；
 *   实例级：先搜终端 → 从终端行里拿 `type_id` → 再取该类型的 item_code。
 * 顺序反了会拿到空候选（终端本身不带物模型）。
 */
export function SnapshotBindingForm({ open, onClose, onCreated }: Props) {
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<Scope>('type');
  const [typeID, setTypeID] = useState('');
  const [terminalID, setTerminalID] = useState('');
  const [terminalTypeID, setTerminalTypeID] = useState('');
  const [keyword, setKeyword] = useState('');
  const [debounced, setDebounced] = useState('');
  const [itemCode, setItemCode] = useState('');
  const [formError, setFormError] = useState<string | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(keyword.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [keyword]);

  // 终端搜索：按 ID 或名称，选中后把 type_id 一起带回来（级联的第二跳）。
  const terminals = useQuery({
    queryKey: ['bindings', 'terminals', debounced],
    queryFn: ({ signal }) => fetchList('terminals', { q: debounced, pageSize: 10 }, signal),
    enabled: open && scope === 'terminal' && debounced !== '',
    placeholderData: (prev) => prev,
  });

  const effectiveTypeID = scope === 'type' ? typeID : terminalTypeID;

  // 数据项候选：所选类型的物模型声明（按 type_id 固定过滤）。
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
  const itemOptions = (properties.data?.rows ?? []).map((row) => ({
    value: String(row.item_code ?? ''),
    label: row.name ? `${String(row.item_code)}（${String(row.name)}）` : String(row.item_code ?? ''),
  }));

  const save = useMutation({
    mutationFn: (body: RowData) => createRow(BINDINGS_ENTITY, body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['list', BINDINGS_ENTITY] });
      onCreated();
      onClose();
      reset();
    },
    onError: (error) => {
      // 后端会因「数据项不属于该类型」返回 400 + 原因：原样显示在表单内，抽屉不关。
      setFormError(error instanceof ApiError ? error.detail : String(error));
    },
  });

  const reset = () => {
    setScope('type');
    setTypeID('');
    setTerminalID('');
    setTerminalTypeID('');
    setKeyword('');
    setDebounced('');
    setItemCode('');
    setFormError(null);
  };

  const submit = () => {
    if (effectiveTypeID === '') {
      setFormError(scope === 'type' ? '请选择终端类型' : '请先选择终端');
      return;
    }
    if (itemCode === '') {
      setFormError('请选择数据项');
      return;
    }
    setFormError(null);
    const body: RowData =
      scope === 'type'
        ? { device_type_id: typeID, item_code: itemCode }
        : { terminal_id: terminalID, item_code: itemCode };
    save.mutate(body);
  };

  const switchScope = (next: Scope) => {
    setScope(next);
    setItemCode('');
    setFormError(null);
  };

  return (
    <Drawer
      open={open}
      title="新建快照绑定"
      onClose={onClose}
      description="作用域二选一：整类终端或单个终端；数据项来自所选终端类型的终端属性。"
      footer={
        <>
          <button type="button" className="btn" onClick={onClose} disabled={save.isPending}>
            取消
          </button>
          <button
            type="button"
            className="btn btn-primary"
            onClick={submit}
            disabled={save.isPending}
            aria-busy={save.isPending}
          >
            保存
          </button>
        </>
      }
    >
      <fieldset className="field m-0 border-0 p-0">
        <legend className="mb-1 p-0 text-body">作用域</legend>
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
            onChange={(next) => {
              setTypeID(next);
              setItemCode('');
            }}
            allowEmpty={false}
          />
        </div>
      ) : (
        <div className="field">
          <label htmlFor="binding-terminal">终端</label>
          <input
            id="binding-terminal"
            className="input h-8"
            type="search"
            placeholder="按终端 ID 或名称搜索"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
          {terminalID !== '' && (
            <p className="m-0 text-meta text-muted">
              已选终端：{terminalID}
              {terminalTypeID ? `（类型 ${terminalTypeID}）` : ''}
            </p>
          )}
          {debounced !== '' && terminals.isFetching && (
            <p className="m-0 text-meta text-muted" role="status">正在搜索终端…</p>
          )}
          {debounced !== '' && !terminals.isFetching && (
            <ul className="m-0 flex list-none flex-col gap-1 p-0" aria-label="终端候选">
              {(terminals.data?.rows ?? []).map((row) => (
                <li key={String(row.id)}>
                  <button
                    type="button"
                    className="btn btn-link"
                    aria-pressed={String(row.id) === terminalID}
                    onClick={() => {
                      setTerminalID(String(row.id));
                      setTerminalTypeID(String(row.type_id ?? ''));
                      setItemCode('');
                      setFormError(null);
                    }}
                  >
                    {String(row.id)}（{String(row.name ?? '未命名')}）
                  </button>
                </li>
              ))}
              {(terminals.data?.rows ?? []).length === 0 && (
                <li className="text-meta text-muted">没有匹配的终端</li>
              )}
            </ul>
          )}
        </div>
      )}

      <div className="field">
        <label htmlFor="binding-item">数据项</label>
        <select
          id="binding-item"
          className="select h-8"
          value={itemCode}
          disabled={effectiveTypeID === ''}
          onChange={(event) => setItemCode(event.target.value)}
        >
          <option value="">{effectiveTypeID === '' ? '先选择目标' : '选择数据项'}</option>
          {itemOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
        <p className="m-0 text-meta text-muted">候选来自该终端类型的终端属性（物模型声明）。</p>
      </div>

      {formError && (
        <p className="m-0 rounded-sm border border-negative-text bg-negative-bg px-3 py-2 text-negative-text" role="alert">
          {formError}
        </p>
      )}
    </Drawer>
  );
}

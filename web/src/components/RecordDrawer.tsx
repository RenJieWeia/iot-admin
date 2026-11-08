import { Check, Loader2, Pencil, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { ApiError, createRow, deleteRow, updateRow } from '../api/client';
import type { FieldMeta, RowData, Tone } from '../api/types';
import { cn } from '../lib/cn';
import {
  coerceForSubmit,
  emptyValue,
  formatDateTime,
  formatNumber,
  isBlank,
  optionLabel,
  rowTitle,
} from '../lib/format';
import { useRelationLabels } from '../meta';
import { ConfirmDialog } from './ConfirmDialog';
import { Drawer } from './Drawer';
import { RelationSelect } from './RelationSelect';
import { Switch } from './Switch';
import { useToast } from './Toast';

export type RecordMode = 'view' | 'edit' | 'create';

/** 与 _meta 的实体/只读页元数据同形，只取抽屉用得到的字段。 */
export interface RecordEntity {
  name: string;
  title: string;
  fields: FieldMeta[];
  readOnly?: boolean;
}

export interface RecordDrawerProps {
  entity: RecordEntity;
  row: RowData | null;
  mode: RecordMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved?: () => void;
}

const inputClass =
  'h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg outline-none placeholder:text-muted focus:border-primary disabled:opacity-50';
const invalidClass = 'border-negative focus:border-negative';
const primaryButton =
  'inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover disabled:opacity-50';
const secondaryButton =
  'inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50';
const dangerButton =
  'inline-flex h-9 items-center gap-1.5 rounded-md border border-line px-3 text-sm text-negative hover:bg-negative-soft disabled:opacity-50';

const toneClass: Record<Tone, string> = {
  neutral: 'bg-neutral-soft text-muted',
  positive: 'bg-positive-soft text-positive',
  negative: 'bg-negative-soft text-negative',
  warning: 'bg-warning-soft text-warning',
};

function toneOf(tone?: string): Tone {
  return tone === 'positive' || tone === 'negative' || tone === 'warning' ? tone : 'neutral';
}

/** 主键：优先 id，否则取字段表首列（snapshot-settings 用 table_name、六档用 granularity）。 */
function primaryKeyOf(fields: FieldMeta[]): string {
  return fields.some((field) => field.key === 'id') ? 'id' : (fields[0]?.key ?? 'id');
}

function jsonText(value: unknown): string {
  if (isBlank(value)) return '—';
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function errorText(err: unknown): string {
  if (err instanceof ApiError) return err.detail;
  return err instanceof Error ? err.message : '请求失败';
}

/** 表单初值一律转成输入控件能直接消费的形态。 */
function buildInitial(
  fields: FieldMeta[],
  row: RowData | null,
  creating: boolean,
): Record<string, unknown> {
  const form: Record<string, unknown> = {};
  for (const field of fields) {
    if (!field.editable) continue;
    const raw = creating || !row ? undefined : row[field.key];
    if (field.type === 'boolean') {
      form[field.key] = raw === undefined ? emptyValue(field) : Boolean(raw);
    } else if (field.type === 'json') {
      form[field.key] = isBlank(raw) ? '' : jsonText(raw);
    } else {
      form[field.key] = isBlank(raw) ? '' : String(raw);
    }
  }
  return form;
}

export function RecordDrawer({
  entity,
  row,
  mode: modeProp,
  open,
  onOpenChange,
  onSaved,
}: RecordDrawerProps) {
  const toast = useToast();
  const relationLabel = useRelationLabels();
  const [mode, setMode] = useState<RecordMode>(modeProp);
  const [form, setForm] = useState<Record<string, unknown>>({});
  const [initial, setInitial] = useState<Record<string, unknown>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState('');
  const [saving, setSaving] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  // 网关新建后 Broker 只回一次密码：用弹窗展示，确认"已保存"后再关抽屉。
  const [credential, setCredential] = useState<{ username: string; password: string } | null>(null);
  const [deleting, setDeleting] = useState(false);

  const editableFields = useMemo(() => entity.fields.filter((field) => field.editable), [entity.fields]);
  const visibleFields = useMemo(() => entity.fields.filter((field) => field.visible), [entity.fields]);
  const pk = primaryKeyOf(entity.fields);

  useEffect(() => {
    if (!open) return;
    setMode(modeProp);
    const next = buildInitial(entity.fields, row, modeProp === 'create');
    setForm(next);
    setInitial(next);
    setErrors({});
    setFormError('');
    setSaving(false);
    setDeleting(false);
    setConfirmOpen(false);
  }, [open, modeProp, row, entity.fields]);

  const setField = (key: string, value: unknown) => {
    setForm((prev) => ({ ...prev, [key]: value }));
    setErrors((prev) => {
      if (!prev[key]) return prev;
      const next = { ...prev };
      delete next[key];
      return next;
    });
  };

  const validate = (): boolean => {
    const next: Record<string, string> = {};
    for (const field of editableFields) {
      const value = form[field.key];
      if (field.required && isBlank(value)) {
        next[field.key] = `请填写${field.label}`;
        continue;
      }
      if (field.type === 'json' && !isBlank(value)) {
        try {
          JSON.parse(String(value));
        } catch {
          next[field.key] = `${field.label} 不是合法 JSON`;
        }
        continue;
      }
      if (field.type === 'number' && !isBlank(value) && Number.isNaN(Number(value))) {
        next[field.key] = `${field.label} 必须是数字`;
      }
    }
    setErrors(next);
    return Object.keys(next).length === 0;
  };

  const submit = async () => {
    setFormError('');
    if (!validate()) return;
    // 编辑只提交改过的列：避免把未渲染/未触碰的依赖列一起送回去触发二次校验。
    const targets = mode === 'create'
      ? editableFields
      : editableFields.filter((field) => form[field.key] !== initial[field.key]);
    if (mode === 'edit' && targets.length === 0) {
      toast.info('没有需要保存的修改');
      return;
    }
    const body: RowData = {};
    try {
      for (const field of targets) body[field.key] = coerceForSubmit(field, form[field.key]);
    } catch (err) {
      setFormError(errorText(err));
      return;
    }
    const id = row?.[pk];
    if (mode === 'edit' && isBlank(id)) {
      setFormError('缺少主键，无法保存');
      return;
    }
    setSaving(true);
    try {
      const saved = mode === 'create'
        ? await createRow(entity.name, body)
        : await updateRow(entity.name, String(id), body);
      if (mode === 'create' && typeof saved.broker_password === 'string' && saved.broker_password) {
        setCredential({
          username: String(saved.gateway_id ?? body.gateway_id ?? ''),
          password: saved.broker_password,
        });
        onSaved?.();
        return;
      }
      toast.success(mode === 'create' ? `已新建${entity.title}` : `已保存${entity.title}`);
      onSaved?.();
      onOpenChange(false);
    } catch (err) {
      setFormError(errorText(err));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    const id = row?.[pk];
    if (isBlank(id)) {
      setConfirmOpen(false);
      setFormError('缺少主键，无法删除');
      return;
    }
    setDeleting(true);
    setFormError('');
    try {
      await deleteRow(entity.name, String(id));
      toast.success(`已删除${entity.title}`);
      onSaved?.();
      setConfirmOpen(false);
      onOpenChange(false);
    } catch (err) {
      setConfirmOpen(false);
      setFormError(errorText(err));
    } finally {
      setDeleting(false);
    }
  };

  const renderValue = (field: FieldMeta, value: unknown) => {
    if (isBlank(value)) return <span className="text-muted">—</span>;
    switch (field.type) {
      case 'relation':
        return <span>{relationLabel(field.ref, value)}</span>;
      case 'boolean':
        return <span>{optionLabel(field, value)?.label ?? (value ? '是' : '否')}</span>;
      case 'enum': {
        const hit = optionLabel(field, value);
        if (!hit) return <span>{String(value)}</span>;
        return (
          <span
            className={cn(
              'inline-flex items-center rounded-full px-2 py-0.5 text-xs',
              toneClass[toneOf(hit.tone)],
            )}
          >
            {hit.label}
          </span>
        );
      }
      case 'datetime':
        return <span className="tabular">{formatDateTime(value)}</span>;
      case 'number':
        return <span className="tabular">{formatNumber(value)}</span>;
      case 'json':
        return (
          <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-words rounded-md bg-surface-2 p-2 font-mono text-xs">
            {jsonText(value)}
          </pre>
        );
      case 'id':
        return <span className="break-all font-mono text-xs">{String(value)}</span>;
      default:
        return <span className="whitespace-pre-wrap break-words">{String(value)}</span>;
    }
  };

  const renderInput = (field: FieldMeta) => {
    const id = `rec-${field.key}`;
    const value = form[field.key];
    const invalid = Boolean(errors[field.key]);
    const inputCx = cn(inputClass, invalid && invalidClass);
    switch (field.type) {
      case 'boolean':
        return (
          <div className="flex h-9 items-center">
            <Switch
              id={id}
              checked={Boolean(value)}
              onCheckedChange={(checked) => setField(field.key, checked)}
              label={value ? '是' : '否'}
            />
          </div>
        );
      case 'number':
        return (
          <input
            id={id}
            type="number"
            step="any"
            value={String(value ?? '')}
            onChange={(event) => setField(field.key, event.target.value)}
            className={cn(inputCx, 'tabular')}
          />
        );
      case 'json':
        return (
          <textarea
            id={id}
            rows={6}
            value={String(value ?? '')}
            placeholder='{"key": "value"}'
            onChange={(event) => setField(field.key, event.target.value)}
            className={cn(
              'w-full rounded-md border border-line bg-surface px-3 py-2 font-mono text-xs text-fg outline-none placeholder:text-muted focus:border-primary',
              invalid && invalidClass,
            )}
          />
        );
      case 'relation': {
        const target = field.relation ?? field.ref?.entity;
        if (!target) {
          return (
            <input
              id={id}
              type="text"
              value={String(value ?? '')}
              onChange={(event) => setField(field.key, event.target.value)}
              className={inputCx}
            />
          );
        }
        return (
          <RelationSelect
            id={id}
            entity={target}
            value={value}
            onChange={(next) => setField(field.key, next)}
          />
        );
      }
      case 'datetime':
        return (
          <input
            id={id}
            type="text"
            value={String(value ?? '')}
            placeholder="ISO8601，如 2026-01-01T00:00:00Z"
            onChange={(event) => setField(field.key, event.target.value)}
            className={cn(inputCx, 'tabular')}
          />
        );
      case 'enum':
        return (
          <select
            id={id}
            value={String(value ?? '')}
            onChange={(event) => setField(field.key, event.target.value)}
            className={inputCx}
          >
            <option value="">请选择</option>
            {field.options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        );
      default:
        return (
          <input
            id={id}
            type="text"
            value={String(value ?? '')}
            onChange={(event) => setField(field.key, event.target.value)}
            className={inputCx}
          />
        );
    }
  };

  const title =
    mode === 'create'
      ? `新建${entity.title}`
      : mode === 'edit'
        ? `编辑${entity.title}`
        : row
          ? rowTitle(entity.title, row)
          : entity.title;

  const footer =
    mode === 'view' ? (
      entity.readOnly ? undefined : (
        <div className="flex w-full items-center justify-between gap-2">
          <div>
            {row && (
              <button type="button" className={dangerButton} onClick={() => setConfirmOpen(true)}>
                <Trash2 size={14} />
                删除
              </button>
            )}
          </div>
          {row && (
            <button type="button" className={primaryButton} onClick={() => setMode('edit')}>
              <Pencil size={14} />
              编辑
            </button>
          )}
        </div>
      )
    ) : (
      <>
        <button
          type="button"
          className={secondaryButton}
          onClick={() => {
            if (mode === 'edit') {
              setForm(initial);
              setErrors({});
              setFormError('');
              setMode('view');
              return;
            }
            onOpenChange(false);
          }}
        >
          取消
        </button>
        <button type="button" className={primaryButton} disabled={saving} onClick={submit}>
          {saving ? <Loader2 size={14} className="animate-spin" /> : <Check size={14} />}
          保存
        </button>
      </>
    );

  return (
    <>
      <Drawer open={open} onOpenChange={onOpenChange} title={title} footer={footer}>
        {mode === 'view' ? (
          <div className="space-y-4">
            {formError && (
              <p className="rounded-md bg-negative-soft px-3 py-2 text-sm text-negative">{formError}</p>
            )}
            {row ? (
              <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                {visibleFields.map((field) => (
                  <div
                    key={field.key}
                    className={cn('min-w-0', field.type === 'json' && 'sm:col-span-2')}
                  >
                    <dt className="mb-1 text-xs text-muted">{field.label}</dt>
                    <dd className="min-w-0 text-sm text-fg">{renderValue(field, row[field.key])}</dd>
                  </div>
                ))}
              </dl>
            ) : (
              <p className="py-8 text-center text-sm text-muted">暂无数据</p>
            )}
          </div>
        ) : (
          <div className="space-y-4">
            {formError && (
              <p className="rounded-md bg-negative-soft px-3 py-2 text-sm text-negative">{formError}</p>
            )}
            {editableFields.length === 0 && (
              <p className="py-8 text-center text-sm text-muted">该记录没有可编辑字段</p>
            )}
            {editableFields.map((field) => (
              <div
                key={field.key}
                className="grid gap-1.5 sm:grid-cols-[140px_1fr] sm:items-start sm:gap-3"
              >
                <label htmlFor={`rec-${field.key}`} className="text-sm font-medium text-fg sm:pt-2">
                  {field.label}
                  {field.required && <span className="ml-0.5 text-negative">*</span>}
                </label>
                <div className="space-y-1">
                  {renderInput(field)}
                  {errors[field.key] && <p className="text-xs text-negative">{errors[field.key]}</p>}
                </div>
              </div>
            ))}
          </div>
        )}
      </Drawer>
      {credential ? (
        <ConfirmDialog
          open
          danger={false}
          title="网关凭证（仅显示一次）"
          description={
            <span className="block">
              网关已在 Broker 注册，用下面的账号连接。密码只显示这一次，请立即保存。
              <span className="mt-2 block rounded-md bg-surface-2 p-2 font-mono text-xs text-fg">
                用户名：{credential.username}
                <br />
                密码：{credential.password}
              </span>
            </span>
          }
          confirmLabel="我已保存"
          cancelLabel="关闭"
          onConfirm={() => {
            setCredential(null);
            toast.success('已新建网关');
            onOpenChange(false);
          }}
          onOpenChange={(open) => {
            if (!open) {
              setCredential(null);
              toast.success('已新建网关');
              onOpenChange(false);
            }
          }}
        />
      ) : null}
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={`删除${entity.title}`}
        description="删除后无法恢复，确定删除这条记录吗？"
        confirmLabel="删除"
        loading={deleting}
        onConfirm={remove}
      />
    </>
  );
}

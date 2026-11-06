import { useMemo, useState } from 'react';
import { z } from 'zod';
import type { EntityMeta, FieldMeta, RowData } from '../api/types';
import { formFields } from '../lib/fieldMeta';
import { toFormValue, toSubmitValue } from '../lib/format';
import { Drawer } from './Drawer';
import { RelationSelect } from './RelationSelect';
import styles from './Panel.module.css';

interface Props {
  entity: EntityMeta;
  /** 实体页 true；异常只读页不会渲染这个抽屉。 */
  writable: boolean;
  mode: 'new' | 'edit';
  row: RowData | null;
  saving: boolean;
  deleting: boolean;
  error: string | null;
  onClose: () => void;
  onSave: (body: RowData) => void;
  /** 删除入口只发意图，二次确认由页面统一渲染（确认按钮带对象名）。 */
  onDelete: () => void;
}

function initialValues(fields: FieldMeta[], row: RowData | null): Record<string, string> {
  const values: Record<string, string> = {};
  for (const field of fields) {
    const raw = row ? row[field.key] : undefined;
    values[field.key] =
      raw === undefined ? (field.type === 'boolean' ? 'true' : '') : toFormValue(raw);
  }
  return values;
}

/**
 * 校验只为提示，服务端仍以 admin.columns() 白名单与列类型为准。
 * 必填完全由 `field.required` 驱动（后端下发），前端不按字段类型猜。
 */
function buildSchema(fields: FieldMeta[]) {
  const shape: Record<string, z.ZodTypeAny> = {};
  for (const field of fields) {
    const base = z.string().trim();
    if (field.type === 'number') {
      const numeric = base.refine(
        (v) => v === '' || !Number.isNaN(Number(v)),
        `${field.label}必须是数字`,
      );
      shape[field.key] = field.required
        ? base.min(1, `${field.label}必填`).refine(
            (v) => !Number.isNaN(Number(v)),
            `${field.label}必须是数字`,
          )
        : numeric;
    } else {
      shape[field.key] = field.required ? base.min(1, `${field.label}必填`) : base;
    }
  }
  return z.object(shape);
}

/** 对象标识：有名称用「名称（id）」，确认按钮与提示都带它。 */
export function objectLabel(entity: EntityMeta, row: RowData | null): string {
  if (!row) return entity.title;
  const name = row.name ?? row.code ?? row.id ?? '';
  const id = row.id ?? '';
  if (name && id && String(name) !== String(id)) return `${String(name)}（${String(id)}）`;
  return String(name || id || entity.title);
}

/**
 * 增删改抽屉：字段、控件类型与中文标签全部来自 /_meta；
 * 底部固定「取消/保存」，编辑态额外提供删除入口（二次确认按钮含对象名）。
 */
export function RecordDrawer({
  entity,
  writable,
  mode,
  row,
  saving,
  deleting,
  error,
  onClose,
  onSave,
  onDelete,
}: Props) {
  const fields = useMemo(() => formFields(entity), [entity]);
  const [values, setValues] = useState(() => initialValues(fields, row));
  const [errors, setErrors] = useState<Record<string, string>>({});

  const title = mode === 'new' ? `新建${entity.title}` : `编辑${entity.title}`;

  const submit = () => {
    const parsed = buildSchema(fields).safeParse(values);
    if (!parsed.success) {
      const next: Record<string, string> = {};
      for (const issue of parsed.error.issues) {
        const key = String(issue.path[0] ?? '');
        if (key && !next[key]) next[key] = issue.message;
      }
      setErrors(next);
      return;
    }
    setErrors({});
    const body: RowData = {};
    for (const field of fields) {
      body[field.key] = toSubmitValue(field, values[field.key] ?? '');
    }
    onSave(body);
  };

  const setValue = (key: string, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  };

  const renderControl = (field: FieldMeta) => {
    const id = `field-${field.key}`;
    if (field.ref) {
      return (
        <RelationSelect
          field={field}
          id={id}
          value={values[field.key] ?? ''}
          onChange={(value) => setValue(field.key, value)}
          invalid={Boolean(errors[field.key])}
        />
      );
    }
    if (field.type === 'boolean' || field.type === 'enum') {
      return (
        <select
          id={id}
          className="select"
          value={values[field.key] ?? ''}
          onChange={(event) => setValue(field.key, event.target.value)}
          aria-invalid={Boolean(errors[field.key])}
          aria-describedby={errors[field.key] ? `${id}-error` : undefined}
        >
          <option value="">未设置</option>
          {(field.options ?? []).map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      );
    }
    if (field.type === 'json') {
      return (
        <textarea
          id={id}
          className="input"
          rows={4}
          value={values[field.key] ?? ''}
          onChange={(event) => setValue(field.key, event.target.value)}
        />
      );
    }
    return (
      <input
        id={id}
        className="input"
        type={field.type === 'number' ? 'number' : 'text'}
        value={values[field.key] ?? ''}
        onChange={(event) => setValue(field.key, event.target.value)}
        aria-invalid={Boolean(errors[field.key])}
        aria-describedby={errors[field.key] ? `${id}-error` : undefined}
      />
    );
  };

  return (
    <Drawer
      open
      title={title}
      onClose={onClose}
      description={
        mode === 'new'
          ? `填写${entity.title}字段后保存`
          : `修改${objectLabel(entity, row)}的字段后保存`
      }
      footer={
        <>
          {mode === 'edit' && writable && (
            <button
              type="button"
              className={`btn btn-danger ${styles.drawerFooterDanger}`}
              onClick={onDelete}
              disabled={saving || deleting}
            >
              删除
            </button>
          )}
          <button type="button" className="btn" onClick={onClose} disabled={saving}>
            取消
          </button>
          <button type="button" className="btn btn-primary" onClick={submit} disabled={saving}>
            {saving ? '保存中…' : '保存'}
          </button>
        </>
      }
    >
      {error && (
        <p className={styles.formError} role="alert">
          保存失败：{error}
        </p>
      )}
      {fields.map((field) => {
        const id = `field-${field.key}`;
        return (
          <div key={field.key} className={errors[field.key] ? 'field field-error' : 'field'}>
            <label htmlFor={id}>
              {field.label}
              {field.required ? '（必填）' : ''}
            </label>
            {renderControl(field)}
            {errors[field.key] && (
              <p id={`${id}-error`} className={styles.formError}>
                {errors[field.key]}
              </p>
            )}
          </div>
        );
      })}
    </Drawer>
  );
}

import { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Select, TextField } from '@radix-ui/themes';
import { z } from 'zod';
import type { EntityMeta, FieldMeta, RowData } from '../api/types';
import { formFields } from '../lib/fieldMeta';
import { EMPTY_SELECT_VALUE, fromSelectValue, toSelectValue } from '../lib/radixSelect';
import { toFormValue, toSubmitValue } from '../lib/format';
import * as Checkbox from '@radix-ui/react-checkbox';
import { Drawer } from './Drawer';
import { RelationSelect } from './RelationSelect';

interface Props {
  entity: EntityMeta;
  /**
   * 1107022：抽屉改为**常驻挂载 + open 开关**（原来是 `{cond && <RecordDrawer/>}` 条件挂载）。
   * 条件挂载会让父组件在关闭瞬间把整个组件卸掉，Radix Presence 根本没机会播退场动画。
   * 挂载后内容只在 open=true 时通过 Portal 渲染，关闭期间不发任何请求（组件内无数据 hook）。
   */
  open: boolean;
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
  /**
   * 锁定字段：渲染成只读输入框（灰显、不可编辑），提交时仍带上它的值。
   * 供「终端类型详情页 → 新建属性」把 type_id 钉死；不传时行为与之前完全一致。
   */
  lockedFields?: string[];
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
  open,
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
  lockedFields,
}: Props) {
  const fields = useMemo(() => formFields(entity), [entity]);
  const [values, setValues] = useState(() => initialValues(fields, row));
  const [errors, setErrors] = useState<Record<string, string>>({});

  /**
   * 常驻挂载带来的必然后果：useState 只在首次挂载时取初值，第二次打开会带着上一次的草稿。
   * 这里按「打开时的表单标识（mode + row.id）」在真正打开的那一刻重新播种；
   * 关闭期间不重置（退场动画还在显示内容），关闭时只清掉标记，下次打开（哪怕同一个 row）会再播种。
   */
  const formKey = `${mode}:${String(row?.id ?? '')}`;
  const seededRef = useRef<string | null>(null);
  useEffect(() => {
    if (!open) {
      seededRef.current = null;
      return;
    }
    if (seededRef.current === formKey) return;
    seededRef.current = formKey;
    setValues(initialValues(fields, row));
    setErrors({});
  }, [open, formKey, fields, row]);

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

  const isLocked = (field: FieldMeta) => lockedFields?.includes(field.key) ?? false;

  const renderControl = (field: FieldMeta) => {
    const id = `field-${field.key}`;
    if (isLocked(field)) {
      // 锁定值来自调用方传入的 row（新建属性时 type_id 由详情页固定注入）。
      return (
        <TextField.Root
          id={id}
          size="3"
          value={values[field.key] ?? ''}
          readOnly
          disabled
          aria-readonly="true"
        />
      );
    }
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
    if (field.type === 'boolean') {
      return (
        <div className="flex h-8 items-center gap-2">
          <Checkbox.Root
            id={id}
            checked={values[field.key] === '' ? 'indeterminate' : values[field.key] === 'true'}
            onCheckedChange={(checked) => setValue(field.key, checked === true ? 'true' : 'false')}
            aria-invalid={Boolean(errors[field.key])}
            aria-describedby={errors[field.key] ? `${id}-error` : undefined}
            className="relative h-5 w-9 rounded-full border border-border-strong bg-surface-2 transition-colors data-[state=checked]:border-primary data-[state=checked]:bg-primary"
          >
            <Checkbox.Indicator asChild>
              <span className="pointer-events-none absolute right-0.5 top-0.5 block h-4 w-4 rounded-full bg-surface shadow-card" />
            </Checkbox.Indicator>
          </Checkbox.Root>
          <label htmlFor={id} className="text-sm text-muted">
            {values[field.key] === '' ? '未设置' : values[field.key] === 'true' ? '是' : '否'}
          </label>
        </div>
      );
    }
    if (field.type === 'enum') {
      // 空选项「未设置」用哨兵承载（Radix 的 Select.Item 不接受 value=""），见 lib/radixSelect.ts。
      return (
        <Select.Root
          size="3"
          value={toSelectValue(values[field.key])}
          onValueChange={(value) => setValue(field.key, fromSelectValue(value))}
        >
          <Select.Trigger
            id={id}
            placeholder="未设置"
            aria-label={field.label}
            aria-invalid={Boolean(errors[field.key])}
            aria-describedby={errors[field.key] ? `${id}-error` : undefined}
          />
          <Select.Content position="popper" sideOffset={4}>
            <Select.Item value={EMPTY_SELECT_VALUE}>未设置</Select.Item>
            {(field.options ?? []).map((option) => (
              <Select.Item key={option.value} value={option.value}>
                {option.label}
              </Select.Item>
            ))}
          </Select.Content>
        </Select.Root>
      );
    }
    if (field.type === 'json') {
      return (
        // Radix Themes 没有 TextArea 组件：保留原生 textarea，用等价工具类维持与 TextField 一致的外观
        // （同一套 surface 底 + 1px 边框 + 6px 圆角），P5 若引入 Radix 的 TextArea 再替换。
        <textarea
          id={id}
          className="min-h-24 w-full rounded-sm border border-border bg-surface px-2 py-1 text-base text-text"
          rows={4}
          value={values[field.key] ?? ''}
          onChange={(event) => setValue(field.key, event.target.value)}
        />
      );
    }
    return (
      <TextField.Root
        id={id}
        size="3"
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
      open={open}
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
            <Button
              type="button"
              onClick={onDelete}
              disabled={saving || deleting}
              variant="soft"
              size="3"
              color="red"
              className="touch-hit mr-auto"
            >
              删除
            </Button>
          )}
          <Button
            type="button"
            onClick={onClose}
            disabled={saving}
            variant="surface"
            size="3"
            className="touch-hit"
          >
            取消
          </Button>
          {/* 文案固定「保存」，忙碌态只由 disabled + aria-busy 表达，宽度不跳动（§5.1）。 */}
          <Button
            type="button"
            onClick={submit}
            disabled={saving}
            aria-busy={saving}
            variant="solid"
            size="3"
            className="touch-hit"
          >
            保存
          </Button>
        </>
      }
    >
      <p className="m-0 text-sm text-muted">
        <span className="text-danger" aria-hidden="true">
          *
        </span>{' '}
        为必填项
      </p>
      {error && (
        <p className="m-0 rounded-sm border border-negative-text bg-negative-bg px-3 py-2 text-negative-text" role="alert">
          保存失败：{error}
        </p>
      )}
      {fields.map((field) => {
        const id = `field-${field.key}`;
        return (
          <div key={field.key} className={errors[field.key] ? 'field field-error' : 'field'}>
            <label htmlFor={id} className="flex items-center gap-1">
              <span>{field.label}</span>
              {field.required && (
                <span className="text-danger" aria-hidden="true">
                  *
                </span>
              )}
            </label>
            {renderControl(field)}
            {isLocked(field) && (
              <p className="m-0 text-sm text-muted">由终端类型固定，不可编辑</p>
            )}
            {errors[field.key] && (
              <p
                id={`${id}-error`}
                className="m-0 rounded-sm border border-negative-text bg-negative-bg px-3 py-2 text-negative-text"
              >
                {errors[field.key]}
              </p>
            )}
          </div>
        );
      })}
    </Drawer>
  );
}

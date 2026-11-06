import * as Select from '@radix-ui/react-select';
import { useFieldOptions } from '../api/entities';
import type { FieldMeta } from '../api/types';

interface Props {
  field: FieldMeta;
  value: string;
  onChange: (value: string) => void;
  id?: string;
  invalid?: boolean;
  /** 允许「未设置」空选项（非必填关系）。 */
  allowEmpty?: boolean;
}

/** Radix Select 不允许空字符串作为选项值，用哨兵映射回 ''。 */
const EMPTY_VALUE = '__empty__';

/**
 * 关联字段下拉：选项来自 GET /admin/v1/options/{ref.entity}（上限 1000），
 * 展示 label、提交 value，不在前端硬编码任何实体名称。
 */
export function RelationSelect({ field, value, onChange, id, invalid = false, allowEmpty = true }: Props) {
  const target = field.ref?.entity;
  const query = useFieldOptions(target ? [target] : []);
  const options = (target ? query.data?.[target] : undefined) ?? [];

  return (
    <>
      <Select.Root
        value={value === '' ? EMPTY_VALUE : value}
        onValueChange={(next) => onChange(next === EMPTY_VALUE ? '' : next)}
      >
        <Select.Trigger
          id={id}
          aria-label={field.label}
          aria-invalid={invalid}
          aria-describedby={query.isError ? `${id ?? field.key}-options-error` : undefined}
          className="flex h-9 w-full items-center justify-between rounded-sm border border-border bg-surface px-2 text-left"
        >
          <Select.Value placeholder="未设置" />
          <Select.Icon aria-hidden="true">▾</Select.Icon>
        </Select.Trigger>
        <Select.Portal>
          <Select.Content
            position="popper"
            sideOffset={4}
            className="z-50 max-h-72 overflow-auto rounded-sm border border-border bg-surface"
          >
            <Select.Viewport className="p-1">
              {allowEmpty && (
                <Select.Item value={EMPTY_VALUE} className="cursor-pointer rounded-sm px-2 py-1 outline-none">
                  <Select.ItemText>未设置</Select.ItemText>
                </Select.Item>
              )}
              {options.map((option) => (
                <Select.Item
                  key={option.value}
                  value={option.value}
                  className="cursor-pointer rounded-sm px-2 py-1 outline-none"
                >
                  <Select.ItemText>{option.label}</Select.ItemText>
                </Select.Item>
              ))}
            </Select.Viewport>
          </Select.Content>
        </Select.Portal>
      </Select.Root>
      {query.isError && (
        <p id={`${id ?? field.key}-options-error`} className="m-0 text-muted" role="status">
          关联选项加载失败，可先保存其他字段后重试。
        </p>
      )}
    </>
  );
}

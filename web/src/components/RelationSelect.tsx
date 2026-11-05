import type { FieldMeta } from '../api/types';
import { useFieldOptions } from '../api/entities';

interface Props {
  field: FieldMeta;
  value: string;
  onChange: (value: string) => void;
  id?: string;
  invalid?: boolean;
  /** 允许「未设置」空选项（非必填关系）。 */
  allowEmpty?: boolean;
}

/**
 * 关联字段下拉：选项来自 GET /admin/v1/options/{ref.entity}（上限 1000），
 * 展示 label、提交 value，不在前端硬编码任何实体名称。
 */
export function RelationSelect({
  field,
  value,
  onChange,
  id,
  invalid = false,
  allowEmpty = true,
}: Props) {
  const target = field.ref?.entity;
  const query = useFieldOptions(target ? [target] : []);
  const options = (target ? query.data?.[target] : undefined) ?? [];

  return (
    <>
      <select
        id={id}
        className="select"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={query.isError ? `${id ?? field.key}-options-error` : undefined}
      >
        {allowEmpty && <option value="">未设置</option>}
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {query.isError && (
        <p id={`${id ?? field.key}-options-error`} className="muted" role="status">
          关联选项加载失败，可先保存其他字段后重试。
        </p>
      )}
    </>
  );
}

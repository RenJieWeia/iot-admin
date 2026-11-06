import { useEffect, useMemo, useRef, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import type { FieldMeta, Option } from '../api/types';
import { advancedFilters, quickFilters, type FieldHolder } from '../lib/fieldMeta';
import {
  clearFilters,
  patchQuery,
  removeFilterAt,
  type ActiveFilter,
  type QueryState,
} from '../lib/queryState';
import { formatFilterValue, operatorLabel } from '../lib/format';

interface Props {
  entity: FieldHolder;
  query: QueryState;
  /** 关联字段的 value→label 映射，用于筛选项与标签展示。 */
  relationOptions: Record<string, Record<string, string>>;
  /** replace=true 时不写浏览器历史（关键词连续输入用）。 */
  onChange: (next: QueryState, replace?: boolean) => void;
}

function relationOptionList(labels: Record<string, string> | undefined): Option[] {
  if (!labels) return [];
  return Object.entries(labels)
    .sort(([a], [b]) => a.localeCompare(b, 'zh-Hans-CN', { numeric: true }))
    .map(([value, label]) => ({ value, label }));
}

function optionList(
  field: FieldMeta,
  relationOptions: Record<string, Record<string, string>>,
): Option[] {
  if (field.filterType === 'relation' || field.filterType === 'remoteSelect') {
    return relationOptionList(relationOptions[field.key]);
  }
  return field.options ?? [];
}

function valuesOf(query: QueryState, field: string, op: string): string[] {
  return query.filters.find((f) => f.field === field && f.op === op)?.values ?? [];
}

/** 单字段筛选控件：控件类型由 filterType 决定，取值后立即写回 URL（变化回第一页）。 */
function FilterControl({
  field,
  query,
  relationOptions,
  onChange,
}: {
  field: FieldMeta;
  query: QueryState;
  relationOptions: Record<string, Record<string, string>>;
  onChange: (next: QueryState) => void;
}) {
  const options = optionList(field, relationOptions);
  const inputId = `filter-${field.key}`;

  const commit = (op: ActiveFilter['op'], values: string[]) => {
    const rest = query.filters.filter((f) => f.field !== field.key);
    const next: ActiveFilter[] = values.length ? [...rest, { field: field.key, op, values }] : rest;
    onChange(patchQuery(query, { filters: next }));
  };

  if (
    field.filterType === 'boolean' ||
    field.filterType === 'multiSelect' ||
    field.filterType === 'relation' ||
    field.filterType === 'remoteSelect'
  ) {
    const selected = valuesOf(query, field.key, field.operators[0] ?? '');
    const multi = field.filterType !== 'boolean';
    return (
      <div className="field">
        <label htmlFor={inputId}>{field.label}</label>
        {multi ? (
          <select
            id={inputId}
            className="select h-8"
            multiple
            size={3}
            value={selected}
            onChange={(event) =>
              commit(
                field.operators[0] ?? 'in',
                Array.from(event.target.selectedOptions, (option) => option.value),
              )
            }
          >
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        ) : (
          <select
            id={inputId}
            className="select h-8"
            value={selected[0] ?? ''}
            onChange={(event) =>
              commit(field.operators[0] ?? 'eq', event.target.value ? [event.target.value] : [])
            }
          >
            <option value="">全部</option>
            {options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        )}
      </div>
    );
  }

  if (field.filterType === 'numberRange' || field.filterType === 'dateRange') {
    const isNumber = field.filterType === 'numberRange';
    const gte = valuesOf(query, field.key, 'gte')[0] ?? '';
    const lte = valuesOf(query, field.key, 'lte')[0] ?? '';
    const commitRange = (nextGte: string, nextLte: string) => {
      const rest = query.filters.filter((f) => f.field !== field.key);
      const next: ActiveFilter[] = [...rest];
      if (nextGte) next.push({ field: field.key, op: 'gte', values: [nextGte] });
      if (nextLte) next.push({ field: field.key, op: 'lte', values: [nextLte] });
      onChange(patchQuery(query, { filters: next }));
    };
    return (
      <div className="field">
        <label htmlFor={`${inputId}-gte`}>{field.label}（范围）</label>
        <div className="flex min-h-9 flex-wrap items-center gap-2 rounded-sm border border-border p-2">
          <input
            id={`${inputId}-gte`}
            className={
              isNumber
                ? 'input option-group-field number-input h-8'
                : 'input option-group-field date-input h-8'
            }
            type={isNumber ? 'number' : 'text'}
            placeholder={isNumber ? '最小值' : 'YYYY-MM-DD'}
            aria-label={`${field.label} 下限`}
            defaultValue={gte}
            onKeyDown={(event) => {
              if (event.key === 'Enter') commitRange(event.currentTarget.value, lte);
            }}
            onBlur={(event) => commitRange(event.currentTarget.value, lte)}
          />
          <span aria-hidden="true">–</span>
          <input
            className={
              isNumber
                ? 'input option-group-field number-input h-8'
                : 'input option-group-field date-input h-8'
            }
            type={isNumber ? 'number' : 'text'}
            placeholder={isNumber ? '最小值' : 'YYYY-MM-DD'}
            aria-label={`${field.label} 上限`}
            defaultValue={lte}
            onKeyDown={(event) => {
              if (event.key === 'Enter') commitRange(gte, event.currentTarget.value);
            }}
            onBlur={(event) => commitRange(gte, event.currentTarget.value)}
          />
        </div>
      </div>
    );
  }

  const current = valuesOf(query, field.key, field.operators[0] ?? '').join('、');
  return (
    <div className="field">
      <label htmlFor={inputId}>{field.label}</label>
      <input
        id={inputId}
        className="input h-8"
        type="search"
        defaultValue={current}
        placeholder={`按${field.label}筛选`}
        onKeyDown={(event) => {
          if (event.key !== 'Enter') return;
          const value = event.currentTarget.value.trim();
          commit(field.operators[0] ?? 'contains', value ? [value] : []);
        }}
        onBlur={(event) => {
          const value = event.currentTarget.value.trim();
          if (value === current) return;
          commit(field.operators[0] ?? 'contains', value ? [value] : []);
        }}
      />
    </div>
  );
}

/** 已生效条件必须始终以标签外显：可单删、可全清（table-query §5）。 */
export function ActiveFilterChips({ entity, query, relationOptions, onChange }: Props) {
  const chips = query.filters
    .map((filter) => {
      const field = entity.fields.find((f) => f.key === filter.field);
      if (!field) return null;
      const op = filter.op || field.operators[0] || '';
      return {
        key: `${filter.field}:${filter.op}`,
        label: field.label,
        opLabel: operatorLabel(field, op),
        value: formatFilterValue(field, filter.values, relationOptions[filter.field]),
        field: filter.field,
        op: filter.op,
      };
    })
    .filter((chip): chip is NonNullable<typeof chip> => chip !== null);

  if (chips.length === 0 && !query.q) return null;

  return (
    <ul className="m-0 mb-3 flex list-none flex-wrap items-center gap-2 py-2" aria-label="已生效筛选条件">
      <li className="text-sm text-muted" aria-hidden="true">
        筛选：
      </li>
      {query.q && (
        <li className="inline-flex max-w-80 items-center gap-1 rounded-sm border border-border bg-surface px-2 py-1 text-sm">
          <span className="text-sm text-muted">关键词</span>
          <span className="overflow-hidden text-ellipsis whitespace-nowrap" title={query.q}>
            {query.q}
          </span>
          <button
            type="button"
            className="min-h-6 cursor-pointer border-0 bg-transparent px-1 leading-none text-muted hover:text-negative-text"
            aria-label={`移除关键词筛选 ${query.q}`}
            onClick={() => onChange(patchQuery(query, { q: '' }))}
          >
            ✕
          </button>
        </li>
      )}
      {chips.map((chip) => (
        <li key={chip.key} className="inline-flex max-w-80 items-center gap-1 rounded-sm border border-border bg-surface px-2 py-1 text-sm">
          <span className="text-sm text-muted">{chip.label}</span>
          <span className="text-sm text-muted">{chip.opLabel}</span>
          <span className="overflow-hidden text-ellipsis whitespace-nowrap" title={chip.value}>
            {chip.value}
          </span>
          <button
            type="button"
            className="min-h-6 cursor-pointer border-0 bg-transparent px-1 leading-none text-muted hover:text-negative-text"
            aria-label={`移除筛选 ${chip.label} ${chip.opLabel} ${chip.value}`}
            onClick={() => onChange(removeFilterAt(query, chip.field, chip.op))}
          >
            ✕
          </button>
        </li>
      ))}
      <li>
        <button type="button" className="btn btn-link" onClick={() => onChange(clearFilters(query))}>
          清除全部筛选
        </button>
      </li>
    </ul>
  );
}

export function FilterBar({ entity, query, relationOptions, onChange }: Props) {
  const quick = useMemo(() => quickFilters(entity), [entity]);
  const advanced = useMemo(() => advancedFilters(entity), [entity]);
  const [keyword, setKeyword] = useState(query.q);
  const [dialogOpen, setDialogOpen] = useState(false);
  const debounceRef = useRef<number | null>(null);

  useEffect(() => {
    setKeyword(query.q);
  }, [query.q]);

  useEffect(
    () => () => {
      if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    },
    [],
  );

  const commitKeyword = (value: string) => {
    if (value === query.q) return;
    // 关键词是连续输入，写历史会把返回键刷满；用 replace 只保留最新一次。
    onChange(patchQuery(query, { q: value }), true);
  };

  return (
    <>
      <div className="mb-3 flex items-end gap-3">
        <div className="filter-grid flex-1">
        <div className="field filter-span-2">
          <label htmlFor="filter-keyword">关键词</label>
          <input
            id="filter-keyword"
            className="input h-8"
            type="search"
            value={keyword}
            placeholder="在可搜索的文本列中匹配"
            onChange={(event) => {
              const value = event.target.value;
              setKeyword(value);
              if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
              debounceRef.current = window.setTimeout(() => commitKeyword(value.trim()), 300);
            }}
            onKeyDown={(event) => {
              if (event.key !== 'Enter') return;
              if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
              commitKeyword(event.currentTarget.value.trim());
            }}
          />
        </div>
          {quick.map((field) => (
            <FilterControl
              key={field.key}
              field={field}
              query={query}
              relationOptions={relationOptions}
              onChange={onChange}
            />
          ))}
        </div>
        {advanced.length > 0 && (
          <button
            type="button"
            className="btn mb-0.5 h-8 shrink-0"
            onClick={() => setDialogOpen(true)}
          >
            更多筛选
          </button>
        )}
      </div>

      <ActiveFilterChips
        entity={entity}
        query={query}
        relationOptions={relationOptions}
        onChange={onChange}
      />

      <Dialog.Root open={dialogOpen} onOpenChange={setDialogOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-overlay" />
          <Dialog.Content
            className="confirm-panel fixed left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 flex-col gap-3 rounded-md bg-surface p-5"
            aria-describedby={undefined}
          >
            <Dialog.Title className="m-0 text-lg">更多筛选</Dialog.Title>
            <div className="grid grid-cols-2 gap-4">
              {advanced.map((field) => (
                <FilterControl
                  key={field.key}
                  field={field}
                  query={query}
                  relationOptions={relationOptions}
                  onChange={onChange}
                />
              ))}
            </div>
            <div className="mt-2 flex justify-end gap-2">
              <button type="button" className="btn" onClick={() => setDialogOpen(false)}>
                关闭
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}

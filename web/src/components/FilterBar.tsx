import { useEffect, useState } from 'react';
import * as Checkbox from '@radix-ui/react-checkbox';
import * as DropdownMenu from '@radix-ui/react-dropdown-menu';
import * as Popover from '@radix-ui/react-popover';
import { Check, ChevronDown, Search, X } from 'lucide-react';
import { fetchOptions } from '../api/client';
import type { FieldMeta, FilterOp, OptionItem } from '../api/types';
import { cn } from '../lib/cn';
import type { FilterCond, QueryState } from '../lib/query';
import { useRelationLabels } from '../meta';

const PRIMARY_FILTER_LIMIT = 5;
const controlClass =
  'w-full rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-fg placeholder:text-muted focus:border-primary';
const chipClass =
  'inline-flex items-center gap-1 rounded-full border border-line bg-surface-2 px-2 py-0.5 text-xs text-fg';

function withField(state: QueryState, field: string, conds: FilterCond[]): QueryState {
  const kept = state.filters.filter((cond) => cond.field !== field);
  return { ...state, filters: [...kept, ...conds.filter((cond) => cond.values.length)], page: 1 };
}

function withoutCond(state: QueryState, target: FilterCond): QueryState {
  return { ...state, filters: state.filters.filter((cond) => cond !== target), page: 1 };
}

function condText(
  field: FieldMeta,
  cond: FilterCond,
  relationLabel: (ref: FieldMeta['ref'], value: unknown) => string,
): string {
  const operator = cond.op ? (field.operatorLabels[cond.op] ?? cond.op) : '';
  const values = cond.values.map((value) => {
    if (field.type === 'relation') return relationLabel(field.ref, value);
    const option = field.options.find((item) => item.value === value);
    return option?.label ?? value;
  });
  return [field.label, operator, values.join('、')].filter(Boolean).join(' ');
}

interface FilterControlProps {
  field: FieldMeta;
  query: QueryState;
  onChange: (next: QueryState) => void;
  onClose: () => void;
}

/** 单个字段的筛选控件：按 filterType 分派，本地草稿在“应用”时写回 QueryState。 */
function FilterControl({ field, query, onChange, onClose }: FilterControlProps) {
  const own = query.filters.filter((cond) => cond.field === field.key);
  const [op, setOp] = useState<FilterOp>(own[0]?.op ?? field.operators[0] ?? 'contains');
  const [text, setText] = useState(own[0]?.values[0] ?? '');
  const [selected, setSelected] = useState<string[]>(own[0]?.values ?? []);
  const [from, setFrom] = useState(own.find((cond) => cond.op === 'gte')?.values[0] ?? '');
  const [to, setTo] = useState(own.find((cond) => cond.op === 'lte')?.values[0] ?? '');
  const [term, setTerm] = useState('');
  const [options, setOptions] = useState<OptionItem[]>([]);

  useEffect(() => {
    if (field.filterType !== 'relation' || !field.ref) return;
    const controller = new AbortController();
    const entity = field.ref.entity;
    const load = async () => {
      try {
        const [found, picked] = await Promise.all([
          fetchOptions(entity, term ? { q: term, limit: 20 } : { limit: 20 }, controller.signal),
          selected.length
            ? fetchOptions(entity, { ids: selected }, controller.signal)
            : Promise.resolve<OptionItem[]>([]),
        ]);
        if (controller.signal.aborted) return;
        const merged: OptionItem[] = [];
        const seen = new Set<string>();
        for (const item of [...picked, ...found]) {
          if (seen.has(item.value)) continue;
          seen.add(item.value);
          merged.push(item);
        }
        setOptions(merged);
      } catch {
        // 选项拉取失败不阻塞列表：让用户先改条件
      }
    };
    void load();
    return () => controller.abort();
  }, [field.filterType, field.ref, term, selected]);

  const clear = () => {
    onChange(withField(query, field.key, []));
    onClose();
  };

  const applyValues = (values: string[]) => {
    setSelected(values);
    const operator = field.operators.includes('in') ? 'in' : field.operators[0];
    onChange(
      withField(
        query,
        field.key,
        values.length ? [{ field: field.key, op: operator, values }] : [],
      ),
    );
  };

  const toggleValue = (value: string) => {
    applyValues(
      selected.includes(value) ? selected.filter((item) => item !== value) : [...selected, value],
    );
  };

  const applyText = () => {
    onChange(
      withField(
        query,
        field.key,
        text.trim() ? [{ field: field.key, op, values: [text.trim()] }] : [],
      ),
    );
    onClose();
  };

  const applyRange = () => {
    const conds: FilterCond[] = [];
    if (from.trim()) conds.push({ field: field.key, op: 'gte', values: [from.trim()] });
    if (to.trim()) conds.push({ field: field.key, op: 'lte', values: [to.trim()] });
    onChange(withField(query, field.key, conds));
    onClose();
  };

  if (field.filterType === 'text') {
    return (
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <DropdownMenu.Root>
            <DropdownMenu.Trigger asChild>
              <button
                type="button"
                className="inline-flex h-8 shrink-0 items-center gap-1 rounded-md border border-line px-2 text-xs text-fg hover:bg-surface-2"
              >
                {field.operatorLabels[op] ?? op}
                <ChevronDown className="size-3" aria-hidden />
              </button>
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content
                align="start"
                sideOffset={4}
                className="z-50 min-w-24 rounded-md border border-line bg-surface p-1 shadow-panel"
              >
                {field.operators.map((operator) => (
                  <DropdownMenu.Item
                    key={operator}
                    onSelect={() => setOp(operator)}
                    className={cn(
                      'cursor-pointer rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-surface-2',
                      operator === op && 'text-primary',
                    )}
                  >
                    {field.operatorLabels[operator] ?? operator}
                  </DropdownMenu.Item>
                ))}
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
          <input
            autoFocus
            value={text}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') applyText();
            }}
            placeholder={`输入${field.label}`}
            aria-label={field.label}
            className={controlClass}
          />
        </div>
        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={clear}
            className="rounded-md px-2 py-1 text-xs text-muted hover:text-fg"
          >
            清除
          </button>
          <button
            type="button"
            onClick={applyText}
            className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-white hover:bg-primary-hover"
          >
            应用
          </button>
        </div>
      </div>
    );
  }

  if (field.filterType === 'numberRange' || field.filterType === 'dateRange') {
    const dateMode = field.filterType === 'dateRange';
    return (
      <div className="flex flex-col gap-2">
        <div className="flex items-center gap-2">
          <input
            type={dateMode ? 'datetime-local' : 'number'}
            step={dateMode ? undefined : 'any'}
            value={from}
            onChange={(event) => setFrom(event.target.value)}
            aria-label={`${field.label} 下限`}
            placeholder="下限"
            className={controlClass}
          />
          <span className="text-xs text-muted">至</span>
          <input
            type={dateMode ? 'datetime-local' : 'number'}
            step={dateMode ? undefined : 'any'}
            value={to}
            onChange={(event) => setTo(event.target.value)}
            aria-label={`${field.label} 上限`}
            placeholder="上限"
            className={controlClass}
          />
        </div>
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted">留空表示不限</span>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={clear}
              className="rounded-md px-2 py-1 text-xs text-muted hover:text-fg"
            >
              清除
            </button>
            <button
              type="button"
              onClick={applyRange}
              className="rounded-md bg-primary px-3 py-1 text-xs font-medium text-white hover:bg-primary-hover"
            >
              应用
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (field.filterType === 'boolean') {
    const current = own[0]?.values[0];
    const items = field.options.length
      ? field.options
      : [
          { value: 'true', label: '是' },
          { value: 'false', label: '否' },
        ];
    return (
      <div className="flex flex-col gap-2">
        <div className="flex gap-2">
          {items.map((item) => (
            <button
              key={item.value}
              type="button"
              aria-pressed={current === item.value}
              onClick={() => applyValues(current === item.value ? [] : [item.value])}
              className={cn(
                'flex-1 rounded-md border px-2 py-1.5 text-sm',
                current === item.value
                  ? 'border-primary bg-primary-soft text-primary'
                  : 'border-line text-fg hover:bg-surface-2',
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={clear}
          className="self-end rounded-md px-2 py-1 text-xs text-muted hover:text-fg"
        >
          清除
        </button>
      </div>
    );
  }

  if (field.filterType === 'relation') {
    return (
      <div className="flex flex-col gap-2">
        <div className="relative">
          <Search
            className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted"
            aria-hidden
          />
          <input
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            placeholder="搜索选项"
            aria-label={`搜索${field.label}`}
            className={cn(controlClass, 'pl-7')}
          />
        </div>
        <div className="max-h-56 overflow-y-auto">
          {options.length ? (
            options.map((option) => (
              <label
                key={option.value}
                className="flex cursor-pointer items-center gap-2 rounded px-1 py-1.5 text-sm hover:bg-surface-2"
              >
                <Checkbox.Root
                  checked={selected.includes(option.value)}
                  onCheckedChange={() => toggleValue(option.value)}
                  className="flex size-4 shrink-0 items-center justify-center rounded border border-line-strong bg-surface data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-white"
                >
                  <Checkbox.Indicator>
                    <Check className="size-3" aria-hidden />
                  </Checkbox.Indicator>
                </Checkbox.Root>
                <span className="truncate">{option.label}</span>
              </label>
            ))
          ) : (
            <p className="px-1 py-2 text-xs text-muted">没有可选数据</p>
          )}
        </div>
        <button
          type="button"
          onClick={clear}
          className="self-end rounded-md px-2 py-1 text-xs text-muted hover:text-fg"
        >
          清除
        </button>
      </div>
    );
  }

  // multiSelect：复选即生效
  return (
    <div className="flex flex-col gap-2">
      <div className="max-h-56 overflow-y-auto">
        {field.options.length ? (
          field.options.map((option) => (
            <label
              key={option.value}
              className="flex cursor-pointer items-center gap-2 rounded px-1 py-1.5 text-sm hover:bg-surface-2"
            >
              <Checkbox.Root
                checked={selected.includes(option.value)}
                onCheckedChange={() => toggleValue(option.value)}
                className="flex size-4 shrink-0 items-center justify-center rounded border border-line-strong bg-surface data-[state=checked]:border-primary data-[state=checked]:bg-primary data-[state=checked]:text-white"
              >
                <Checkbox.Indicator>
                  <Check className="size-3" aria-hidden />
                </Checkbox.Indicator>
              </Checkbox.Root>
              <span className="truncate">{option.label}</span>
            </label>
          ))
        ) : (
          <p className="px-1 py-2 text-xs text-muted">没有可选数据</p>
        )}
      </div>
      <button
        type="button"
        onClick={clear}
        className="self-end rounded-md px-2 py-1 text-xs text-muted hover:text-fg"
      >
        清除
      </button>
    </div>
  );
}

interface FilterBarProps {
  fields: FieldMeta[];
  query: QueryState;
  onChange: (next: QueryState) => void;
}

/** 搜索（300ms 防抖）+ 元数据驱动的筛选控件 + 已选条件 chips。 */
export function FilterBar({ fields, query, onChange }: FilterBarProps) {
  const relationLabel = useRelationLabels();
  const [search, setSearch] = useState(query.q);
  const [openField, setOpenField] = useState<string | null>(null);
  const [showAll, setShowAll] = useState(false);

  useEffect(() => {
    setSearch(query.q);
  }, [query.q]);

  useEffect(() => {
    if (search === query.q) return;
    const timer = window.setTimeout(() => {
      onChange({ ...query, q: search, page: 1 });
    }, 300);
    return () => window.clearTimeout(timer);
  }, [search, query, onChange]);

  const filterFields = fields.filter(
    (field) => field.filterable && field.filterType !== 'none',
  );
  const primary = filterFields.slice(0, PRIMARY_FILTER_LIMIT);
  const shown = showAll ? filterFields : primary;
  const hiddenCount = filterFields.length - primary.length;
  const fieldMap = new Map(fields.map((field) => [field.key, field]));

  const chips = query.filters.flatMap((cond) => {
    const field = fieldMap.get(cond.field);
    if (!field || !cond.values.length) return [];
    return [{ cond, label: condText(field, cond, relationLabel) }];
  });

  const dirty = query.q !== '' || query.filters.length > 0;

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2 px-4 py-3">
        <div className="relative w-full sm:w-72">
          <Search
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted"
            aria-hidden
          />
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索关键字"
            aria-label="搜索关键字"
            className="h-9 w-full rounded-md border border-line bg-surface pr-8 pl-8 text-sm text-fg placeholder:text-muted focus:border-primary"
          />
          {search ? (
            <button
              type="button"
              aria-label="清空搜索"
              onClick={() => {
                setSearch('');
                onChange({ ...query, q: '', page: 1 });
              }}
              className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted hover:text-fg"
            >
              <X className="size-3.5" aria-hidden />
            </button>
          ) : null}
        </div>

        {shown.map((field) => {
          const count = query.filters.filter((cond) => cond.field === field.key).length;
          return (
            <Popover.Root
              key={field.key}
              open={openField === field.key}
              onOpenChange={(open) => setOpenField(open ? field.key : null)}
            >
              <Popover.Trigger asChild>
                <button
                  type="button"
                  className={cn(
                    'inline-flex h-9 items-center gap-1 rounded-md border px-2.5 text-sm',
                    count
                      ? 'border-primary bg-primary-soft text-primary'
                      : 'border-line text-fg hover:bg-surface-2',
                  )}
                >
                  {field.label}
                  {count ? <span className="tabular text-xs">({count})</span> : null}
                  <ChevronDown className="size-3.5 text-muted" aria-hidden />
                </button>
              </Popover.Trigger>
              <Popover.Portal>
                <Popover.Content
                  align="start"
                  sideOffset={6}
                  className="z-50 w-72 rounded-lg border border-line bg-surface p-3 shadow-panel"
                >
                  <p className="mb-2 text-xs font-medium text-muted">{field.label}</p>
                  <FilterControl
                    field={field}
                    query={query}
                    onChange={onChange}
                    onClose={() => setOpenField(null)}
                  />
                </Popover.Content>
              </Popover.Portal>
            </Popover.Root>
          );
        })}

        {hiddenCount > 0 ? (
          <button
            type="button"
            onClick={() => setShowAll((value) => !value)}
            className="inline-flex h-9 items-center gap-1 rounded-md px-2 text-sm text-muted hover:text-fg"
          >
            {showAll ? '收起筛选' : `更多筛选 (${hiddenCount})`}
          </button>
        ) : null}

        {dirty ? (
          <button
            type="button"
            onClick={() => {
              setSearch('');
              onChange({ ...query, q: '', filters: [], page: 1 });
            }}
            className="ml-auto inline-flex h-9 items-center rounded-md px-2 text-sm text-muted hover:text-fg"
          >
            重置
          </button>
        ) : null}
      </div>

      {chips.length ? (
        <div className="flex flex-wrap items-center gap-2 border-t border-line px-4 py-2">
          <span className="text-xs text-muted">已选条件</span>
          {chips.map(({ cond, label }) => (
            <span key={`${cond.field}-${cond.op ?? ''}-${cond.values.join('|')}`} className={chipClass}>
              {label}
              <button
                type="button"
                aria-label={`移除 ${label}`}
                onClick={() => onChange(withoutCond(query, cond))}
                className="rounded p-0.5 text-muted hover:text-fg"
              >
                <X className="size-3" aria-hidden />
              </button>
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

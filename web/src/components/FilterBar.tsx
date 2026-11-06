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
  /** 关联选项加载失败（table-query §5：保留已选条件，局部提示重试，不阻塞表格）。 */
  optionsError?: boolean;
  onRetryOptions?: () => void;
}

/** 范围非法提示：数值比大小，日期按 ISO 字典序（YYYY-MM-DD 可直接比较）；空值表示不限。 */
function rangeProblem(filterType: string, gte: string, lte: string): string | null {
  if (!gte || !lte) return null;
  const invalid = filterType === 'numberRange' ? Number(gte) > Number(lte) : gte > lte;
  if (!invalid) return null;
  return filterType === 'numberRange'
    ? `最小值不能大于最大值（${gte} > ${lte}），已保留输入但未提交查询`
    : `开始日期不能晚于结束日期（${gte} > ${lte}），已保留输入但未提交查询`;
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

/**
 * 字段已生效的值，合并该字段任一 op：删标签/清除全部后控件必须跟着清空，
 * 只认首个 op 会让「URL 里换了 op」的条件在控件里不可见（table-query §2、§10）。
 */
function mergedValues(query: QueryState, field: string): string[] {
  return query.filters.filter((f) => f.field === field).flatMap((f) => f.values);
}

/**
 * 受控草稿：本地输入优先，只有 URL 被外部改动（删标签、清除全部、浏览器后退）时才回灌。
 * 判据是「我最后写进 URL 的值」——两者相同说明这次变化由自己引起，此时回灌会覆盖
 * 正在输入的内容并让光标跳位；不同才说明是外部改的，必须回灌。
 */
function useQueryDraft(value: string) {
  const [draft, setDraft] = useState(value);
  const written = useRef(value);
  useEffect(() => {
    if (value === written.current) return;
    written.current = value;
    setDraft(value);
  }, [value]);
  /** 提交前登记即将写进 URL 的值，压掉这次自变的回灌。 */
  const noteWritten = (next: string) => {
    written.current = next;
  };
  return { draft, setDraft, noteWritten };
}

/** 文本筛选的提交时机：输入 300ms 后写 URL，回车/失焦立即提交（table-query §4，与关键词同实现）。 */
function useDebouncedCommit(commit: (value: string) => void, delay = 300) {
  const timer = useRef<number | null>(null);
  // 回调里取最新一次渲染的 commit：防抖窗口内 query 变了，旧闭包会覆盖掉别的条件。
  const latest = useRef(commit);
  const cancel = () => {
    if (timer.current !== null) window.clearTimeout(timer.current);
  };
  useEffect(() => {
    latest.current = commit;
  });
  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    },
    [],
  );
  return {
    schedule(value: string) {
      cancel();
      timer.current = window.setTimeout(() => latest.current(value), delay);
    },
    flush(value: string) {
      cancel();
      commit(value);
    },
  };
}

/** 单字段筛选控件：控件类型由 filterType 决定，取值后立即写回 URL（变化回第一页）。 */
function FilterControl({
  field,
  query,
  relationOptions,
  onChange,
  optionsError,
  onRetryOptions,
}: {
  field: FieldMeta;
  query: QueryState;
  relationOptions: Record<string, Record<string, string>>;
  onChange: (next: QueryState) => void;
  optionsError?: boolean;
  onRetryOptions?: () => void;
}) {
  const options = optionList(field, relationOptions);
  const inputId = `filter-${field.key}`;

  const commit = (op: ActiveFilter['op'], values: string[]) => {
    const rest = query.filters.filter((f) => f.field !== field.key);
    const next: ActiveFilter[] = values.length ? [...rest, { field: field.key, op, values }] : rest;
    onChange(patchQuery(query, { filters: next }));
  };

  // 草稿必须无条件建好：下面按 filterType 早返回，hooks 不能放进分支里。
  const current = mergedValues(query, field.key).join('、');
  const text = useQueryDraft(current);
  const gte = valuesOf(query, field.key, 'gte')[0] ?? '';
  const lte = valuesOf(query, field.key, 'lte')[0] ?? '';
  const gteDraft = useQueryDraft(gte);
  const lteDraft = useQueryDraft(lte);

  const commitText = (raw: string) => {
    const value = raw.trim();
    // 空值不产生条件；和 URL 一致就不写历史（清除全部后失焦不能把旧值写回来）。
    if (value === current) return;
    text.noteWritten(value);
    commit(field.operators[0] ?? 'contains', value ? [value] : []);
  };
  const textCommit = useDebouncedCommit(commitText);

  const commitRange = (nextGte: string, nextLte: string) => {
    // 非法范围（min>max / 起>止）不发请求：保留用户输入，只在控件下给提示。
    if (rangeProblem(field.filterType, nextGte, nextLte)) return;
    if (nextGte === gte && nextLte === lte) return;
    gteDraft.noteWritten(nextGte);
    lteDraft.noteWritten(nextLte);
    const rest = query.filters.filter((f) => f.field !== field.key);
    const next: ActiveFilter[] = [...rest];
    if (nextGte) next.push({ field: field.key, op: 'gte', values: [nextGte] });
    if (nextLte) next.push({ field: field.key, op: 'lte', values: [nextLte] });
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
        {optionsError && options.length === 0 && (
          <p className="m-0 text-sm text-negative-text">
            选项加载失败，已选条件仍生效。
            <button type="button" className="btn btn-link" onClick={onRetryOptions}>
              重试
            </button>
          </p>
        )}
      </div>
    );
  }

  if (field.filterType === 'numberRange' || field.filterType === 'dateRange') {
    const isNumber = field.filterType === 'numberRange';
    const problem = rangeProblem(field.filterType, gteDraft.draft, lteDraft.draft);
    const errorId = `${inputId}-error`;
    return (
      <div className={problem ? 'field field-error' : 'field'}>
        <label htmlFor={`${inputId}-gte`}>
          {/* 日期是纯日期区间，后端按 +08:00 cast timestamptz，时区必须写清楚（table-query §4）。 */}
          {field.label}（范围{isNumber ? '' : '，+08:00'}）
        </label>
        {/* 范围组只做横排容器：外层的边框/内边距会把它撑到 control-h 以上，
            导致同行 label 与 input 底边错位（两个 input 自己已有 .input 的边框）。 */}
        <div className="flex flex-wrap items-center gap-2">
          <input
            id={`${inputId}-gte`}
            className={
              isNumber
                ? 'input option-group-field number-input h-8'
                : 'input option-group-field date-input h-8'
            }
            type={isNumber ? 'number' : 'date'}
            placeholder={isNumber ? '最小值' : undefined}
            aria-label={`${field.label} 下限`}
            aria-invalid={Boolean(problem)}
            aria-describedby={problem ? errorId : undefined}
            value={gteDraft.draft}
            onChange={(event) => gteDraft.setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') commitRange(event.currentTarget.value, lteDraft.draft);
            }}
            onBlur={(event) => commitRange(event.currentTarget.value, lteDraft.draft)}
          />
          <span aria-hidden="true">–</span>
          <input
            className={
              isNumber
                ? 'input option-group-field number-input h-8'
                : 'input option-group-field date-input h-8'
            }
            type={isNumber ? 'number' : 'date'}
            placeholder={isNumber ? '最大值' : undefined}
            aria-label={`${field.label} 上限`}
            aria-invalid={Boolean(problem)}
            aria-describedby={problem ? errorId : undefined}
            value={lteDraft.draft}
            onChange={(event) => lteDraft.setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') commitRange(gteDraft.draft, event.currentTarget.value);
            }}
            onBlur={(event) => commitRange(gteDraft.draft, event.currentTarget.value)}
          />
        </div>
        {problem && (
          <p
            id={errorId}
            className="m-0 rounded-sm border border-negative-text bg-negative-bg px-3 py-2 text-negative-text"
            role="alert"
          >
            {problem}
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="field">
      <label htmlFor={inputId}>{field.label}</label>
      <input
        id={inputId}
        className="input h-8"
        type="search"
        value={text.draft}
        placeholder={`按${field.label}筛选`}
        onChange={(event) => {
          text.setDraft(event.target.value);
          textCommit.schedule(event.target.value);
        }}
        onKeyDown={(event) => {
          if (event.key !== 'Enter') return;
          textCommit.flush(event.currentTarget.value);
        }}
        onBlur={(event) => textCommit.flush(event.currentTarget.value)}
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

export function FilterBar({
  entity,
  query,
  relationOptions,
  onChange,
  optionsError,
  onRetryOptions,
}: Props) {
  const quick = useMemo(() => quickFilters(entity), [entity]);
  const advanced = useMemo(() => advancedFilters(entity), [entity]);
  const [keyword, setKeyword] = useState(query.q);
  const [dialogOpen, setDialogOpen] = useState(false);

  useEffect(() => {
    setKeyword(query.q);
  }, [query.q]);

  const commitKeyword = (value: string) => {
    if (value === query.q) return;
    // 关键词是连续输入，写历史会把返回键刷满；用 replace 只保留最新一次。
    onChange(patchQuery(query, { q: value }), true);
  };
  // 与字段级文本筛选同一实现：输入 300ms 后写 URL，回车立即提交。
  const keywordCommit = useDebouncedCommit(commitKeyword);

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
              keywordCommit.schedule(value.trim());
            }}
            onKeyDown={(event) => {
              if (event.key !== 'Enter') return;
              keywordCommit.flush(event.currentTarget.value.trim());
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
              optionsError={optionsError}
              onRetryOptions={onRetryOptions}
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
          <Dialog.Overlay className="fixed inset-0 z-40 bg-overlay" />
          <Dialog.Content
            className="confirm-panel fixed left-1/2 top-1/2 z-40 flex -translate-x-1/2 -translate-y-1/2 flex-col gap-3 rounded-md bg-surface p-5"
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
                  optionsError={optionsError}
                  onRetryOptions={onRetryOptions}
                />
              ))}
            </div>
            <div className="mt-2 flex justify-end gap-2">
              <button type="button" className="btn" onClick={() => setDialogOpen(false)}>
                完成
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}

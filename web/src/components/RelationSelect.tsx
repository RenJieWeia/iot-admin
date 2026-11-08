import * as Popover from '@radix-ui/react-popover';
import { Check, ChevronsUpDown, Loader2, Search } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { fetchOptions } from '../api/client';
import type { OptionItem } from '../api/types';
import { cn } from '../lib/cn';

export interface RelationSelectProps {
  /** 关联目标实体名（FieldMeta.relation / ref.entity）。 */
  entity: string;
  value: unknown;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
}

/** 关联字段选择器：搜索走服务端，输入防抖 200ms；已选值单独按 ids 回填展示名。 */
export function RelationSelect({
  entity,
  value,
  onChange,
  placeholder = '请选择',
  disabled,
  id,
}: RelationSelectProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [options, setOptions] = useState<OptionItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [label, setLabel] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const raw = value === null || value === undefined ? '' : String(value);

  // 已选值回填：options 里没有时（比如首次打开就带值）按 ids 精确取一次。
  useEffect(() => {
    if (!raw) {
      setLabel('');
      return;
    }
    const controller = new AbortController();
    fetchOptions(entity, { ids: [raw] }, controller.signal)
      .then((items) => {
        if (!controller.signal.aborted) {
          setLabel(items.find((item) => item.value === raw)?.label ?? raw);
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) setLabel(raw);
      });
    return () => controller.abort();
  }, [entity, raw]);

  // 打开面板后按查询词拉选项；输入停止 200ms 才请求，旧请求直接 abort。
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    const timer = window.setTimeout(
      () => {
        setLoading(true);
        fetchOptions(entity, { q: query.trim() || undefined, limit: 20 }, controller.signal)
          .then((items) => {
            if (!controller.signal.aborted) setOptions(items);
          })
          .catch(() => {
            if (!controller.signal.aborted) setOptions([]);
          })
          .finally(() => {
            if (!controller.signal.aborted) setLoading(false);
          });
      },
      query.trim() ? 200 : 0,
    );
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [open, query, entity]);

  const selectedLabel = options.find((option) => option.value === raw)?.label ?? label;

  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery('');
      }}
    >
      <Popover.Trigger asChild>
        <button
          id={id}
          type="button"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn(
            'flex h-9 w-full items-center justify-between gap-2 rounded-md border border-line bg-surface px-3 text-left text-sm',
            'focus:border-primary disabled:opacity-50',
          )}
        >
          <span className={cn('truncate', !selectedLabel && 'text-muted')}>
            {selectedLabel || placeholder}
          </span>
          <ChevronsUpDown size={14} className="shrink-0 text-muted" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          sideOffset={4}
          align="start"
          className="z-[60] w-[var(--radix-popover-trigger-width)] min-w-[220px] rounded-md border border-line bg-surface p-2 shadow-panel outline-none"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            inputRef.current?.focus();
          }}
        >
          <div className="mb-2 flex items-center gap-2 rounded border border-line px-2">
            <Search size={14} className="shrink-0 text-muted" />
            <input
              ref={inputRef}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="搜索…"
              className="h-8 w-full bg-transparent text-sm text-fg outline-none placeholder:text-muted"
            />
            {loading && <Loader2 size={14} className="shrink-0 animate-spin text-muted" />}
          </div>
          <div role="listbox" className="max-h-60 overflow-y-auto">
            {options.length === 0 && !loading ? (
              <p className="px-2 py-3 text-center text-xs text-muted">无匹配</p>
            ) : (
              options.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  role="option"
                  aria-selected={option.value === raw}
                  onClick={() => {
                    onChange(option.value);
                    setOpen(false);
                    setQuery('');
                  }}
                  className="flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-sm text-fg hover:bg-surface-2"
                >
                  <span className="truncate">{option.label}</span>
                  {option.value === raw && <Check size={14} className="shrink-0 text-primary" />}
                </button>
              ))
            )}
          </div>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

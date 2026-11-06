import * as Popover from '@radix-ui/react-popover';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { useFieldOptions, useOptionSearch } from '../api/entities';
import type { FieldMeta, OptionItem } from '../api/types';

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
 * 关系字段远程搜索下拉（task-012 F4）：Radix Popover + 输入框 + listbox。
 *
 * - 输入 300ms 防抖后按 `q` 查 `/admin/v1/options/{entity}`；q 进 queryKey、fetch 带
 *   AbortSignal，旧响应不会覆盖新结果（只应用最新一次搜索）；
 * - 键盘 ↑↓ 移动、Enter 选中、Esc 关闭并把焦点还给触发按钮；
 * - role=combobox + listbox + option + aria-activedescendant。
 *
 * 已选值的显示文案来自全量 options（与表格列回填同源，≤1000 行）；超过上限时后端不下发该 id，
 * 此时只显示原始 value —— 与表格列「超限显示原始 id」是同一已知限制（本批不解）。
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
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [debounced, setDebounced] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listboxId = useId();

  const allOptions = useFieldOptions(target ? [target] : []);
  const selectedLabel = useMemo(() => {
    const list = (target ? allOptions.data?.[target] : undefined) ?? [];
    return list.find((option) => option.value === value)?.label;
  }, [allOptions.data, target, value]);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(keyword.trim()), 300);
    return () => window.clearTimeout(timer);
  }, [keyword]);

  const search = useOptionSearch(target, debounced, open);
  const options: OptionItem[] = search.data ?? [];

  useEffect(() => {
    setActiveIndex(0);
  }, [debounced, open]);

  const optionId = (optionValue: string) =>
    `${listboxId}-opt-${optionValue.replace(/[^\w-]/g, '_')}`;

  const closeAndRefocus = () => {
    setOpen(false);
    setKeyword('');
    window.requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const choose = (option: OptionItem | undefined) => {
    if (!option) return;
    onChange(option.value);
    closeAndRefocus();
  };

  const clear = () => {
    onChange('');
    setKeyword('');
    triggerRef.current?.focus();
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActiveIndex((index) => Math.min(index + 1, Math.max(options.length - 1, 0)));
      return;
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActiveIndex((index) => Math.max(index - 1, 0));
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      choose(options[activeIndex]);
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      closeAndRefocus();
    }
  };

  const activeOption = options[activeIndex];

  return (
    <div className="flex items-center gap-2">
      <Popover.Root
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          if (!next) setKeyword('');
        }}
      >
        <Popover.Trigger asChild>
          <button
            ref={triggerRef}
            id={id}
            type="button"
            role="combobox"
            aria-expanded={open}
            aria-controls={listboxId}
            aria-invalid={invalid}
            aria-label={field.label}
            className="flex h-9 w-full items-center justify-between rounded-sm border border-border bg-surface px-2 text-left"
          >
            <span className={selectedLabel ?? value ? '' : 'text-muted'}>
              {selectedLabel ?? (value || '未设置')}
            </span>
            <span aria-hidden="true">▾</span>
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content
            align="start"
            sideOffset={4}
            className="z-50 w-80 rounded-sm border border-border bg-surface p-2"
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              inputRef.current?.focus();
            }}
          >
            <input
              ref={inputRef}
              type="text"
              className="input mb-2"
              placeholder="输入关键词搜索"
              value={keyword}
              onChange={(event) => setKeyword(event.target.value)}
              onKeyDown={handleKeyDown}
              role="combobox"
              aria-label={`搜索${field.label}选项`}
              aria-expanded={open}
              aria-controls={listboxId}
              aria-autocomplete="list"
              aria-activedescendant={activeOption ? optionId(activeOption.value) : undefined}
            />
            <ul
              id={listboxId}
              role="listbox"
              aria-label={`${field.label}选项`}
              className="m-0 max-h-60 list-none overflow-auto p-0"
            >
              {search.isFetching && options.length === 0 && (
                <li className="px-2 py-1 text-sm text-muted" role="presentation">
                  正在加载…
                </li>
              )}
              {!search.isFetching && options.length === 0 && (
                <li className="px-2 py-1 text-sm text-muted" role="presentation">
                  没有匹配的选项，换个关键词
                </li>
              )}
              {options.map((option, index) => (
                <li
                  key={option.value}
                  id={optionId(option.value)}
                  role="option"
                  aria-selected={option.value === value}
                  className={
                    index === activeIndex
                      ? 'cursor-pointer rounded-sm bg-neutral-bg px-2 py-1 text-sm'
                      : 'cursor-pointer rounded-sm px-2 py-1 text-sm'
                  }
                  onMouseEnter={() => setActiveIndex(index)}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    choose(option);
                  }}
                >
                  {option.label}
                </li>
              ))}
            </ul>
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      {allowEmpty && value !== '' && (
        <button
          type="button"
          className="btn shrink-0"
          onClick={clear}
          aria-label={`清除${field.label}`}
        >
          清除
        </button>
      )}
      {search.isError && (
        <p className="m-0 text-sm text-muted" role="status">
          选项加载失败，可换个关键词或稍后重试。
        </p>
      )}
    </div>
  );
}

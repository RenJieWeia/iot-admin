import * as Popover from '@radix-ui/react-popover';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { Option } from '../api/types';
import { optionMatches } from '../lib/format';
import { cn } from '../lib/cn';
import { Button, TextField } from '@radix-ui/themes';

interface Props {
  /** 触发按钮的 id：让外层 `<label htmlFor>` 指到可交互元素。 */
  id?: string;
  label: string;
  options: Option[];
  /** 已选值，完全由 QueryState 派生（唯一事实源）。 */
  selected: string[];
  /** 勾选/取消后立即写回 QueryState。 */
  onChange: (values: string[]) => void;
  /**
   * 可选：弹层里提供「全选 / 反选」（作用于当前搜索结果）。
   * 默认 false —— 既有调用方（筛选条）不传，DOM 与行为完全不变。
   */
  allowSelectAll?: boolean;
}

/**
 * 多选筛选的组合框（§5.1 / §14.1）：Radix Popover + 可搜索 listbox。
 *
 * 替换原生 `<select multiple>`（需要 Ctrl 点击、触摸端不可用、选项多了不能搜）。
 * 交互约定：
 * - 触发按钮显示已选标签（空时「全部」），弹层内提供搜索、已选计数、单项移除与清空；
 * - ↑↓ 移动高亮、Enter 切换选中且**不关闭**（多选要连续操作）、Esc 关闭并把焦点还给触发按钮；
 * - 组件内只有「搜索词 / 高亮项 / 开合」这类瞬时状态，选中值不进本地状态。
 */
export function FilterMultiSelect({ id, label, options, selected, onChange, allowSelectAll = false }: Props) {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listboxId = useId();

  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const labelOf = (value: string) => options.find((option) => option.value === value)?.label ?? value;
  const filtered = useMemo(
    () => options.filter((option) => optionMatches(option, keyword)),
    [options, keyword],
  );

  useEffect(() => {
    setActiveIndex(0);
  }, [keyword, open]);

  const close = () => {
    setOpen(false);
    setKeyword('');
    // 与 RelationSelect 同一收尾：关弹层后焦点回到触发按钮。
    window.requestAnimationFrame(() => triggerRef.current?.focus());
  };

  const toggle = (value: string) => {
    onChange(selectedSet.has(value) ? selected.filter((item) => item !== value) : [...selected, value]);
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActiveIndex((index) => Math.min(index + 1, Math.max(filtered.length - 1, 0)));
      return;
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActiveIndex((index) => Math.max(index - 1, 0));
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      const option = filtered[activeIndex];
      if (option) toggle(option.value);
      return;
    }
    if (event.key === 'Escape') {
      // 组合框自己处理 Esc：阻止冒泡，避免把外层「更多筛选 / 筛选抽屉」一起关掉。
      event.preventDefault();
      event.stopPropagation();
      close();
    }
  };

  const optionId = (value: string) => `${listboxId}-opt-${value.replace(/[^\w-]/g, '_')}`;
  const activeOption = filtered[activeIndex];
  const triggerText = selected.length === 0 ? '全部' : selected.map(labelOf).join('、');

  return (
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
          aria-label={label}
          className="flex w-full items-center justify-between gap-2 rounded-sm border border-border bg-surface px-2 text-left text-base"
        >
          <span className={cn('truncate', selected.length === 0 && 'text-muted')}>{triggerText}</span>
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
          <TextField.Root
            ref={inputRef}
            size="3"
            type="text"
            className="mb-2"
            placeholder="输入关键词搜索"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onKeyDown={handleKeyDown}
            role="combobox"
            aria-label={`搜索${label}选项`}
            aria-expanded={open}
            aria-controls={listboxId}
            aria-autocomplete="list"
            aria-activedescendant={activeOption ? optionId(activeOption.value) : undefined}
          />
          {allowSelectAll && filtered.length > 0 && (
            <div className="mb-2 flex items-center gap-2">
              <Button type="button" onMouseDown={(event) => {
                  event.preventDefault();
                  // 全选：把当前搜索结果并入已选（不取消已选但被搜索隐藏的项）。
                  const merged = new Set([...selected, ...filtered.map((option) => option.value)]);
                  onChange(Array.from(merged));
                }} variant="ghost" size="3" className="h-10">
                全选
              </Button>
              <Button type="button" onMouseDown={(event) => {
                  event.preventDefault();
                  // 反选：在当前搜索结果范围内翻转选中状态。
                  const visible = new Set(filtered.map((option) => option.value));
                  const kept = selected.filter((value) => !visible.has(value));
                  const added = filtered
                    .map((option) => option.value)
                    .filter((value) => !selectedSet.has(value));
                  onChange([...kept, ...added]);
                }} variant="ghost" size="3" className="h-10">
                反选
              </Button>
              <span className="text-sm text-muted">作用于当前搜索结果</span>
            </div>
          )}
          {selected.length > 0 && (
            <div className="mb-2 flex flex-wrap items-center gap-1" aria-label={`${label}已选项`}>
              {selected.map((value) => (
                <Button key={value} type="button" aria-label={`移除${label} ${labelOf(value)}`} onMouseDown={(event) => {
                    event.preventDefault();
                    toggle(value);
                  }} variant="ghost" size="3" className="h-10">
                  {labelOf(value)} ✕
                </Button>
              ))}
              <span className="text-sm text-muted">已选 {selected.length} 项</span>
              <Button type="button" onMouseDown={(event) => {
                  event.preventDefault();
                  onChange([]);
                }} variant="ghost" size="3" className="h-10">
                清空
              </Button>
            </div>
          )}
          <ul
            id={listboxId}
            role="listbox"
            aria-label={`${label}选项`}
            className="m-0 max-h-60 list-none overflow-auto p-0"
          >
            {filtered.length === 0 && (
              <li className="px-2 py-1 text-sm text-muted" role="presentation">
                没有匹配的选项，换个关键词
              </li>
            )}
            {filtered.map((option, index) => {
              const isSelected = selectedSet.has(option.value);
              return (
                <li
                  key={option.value}
                  id={optionId(option.value)}
                  role="option"
                  aria-selected={isSelected}
                  className={cn(
                    'cursor-pointer rounded-sm px-2 py-1 text-sm',
                    index === activeIndex && 'bg-neutral-bg',
                  )}
                  onMouseEnter={() => setActiveIndex(index)}
                  onMouseDown={(event) => {
                    event.preventDefault();
                    toggle(option.value);
                  }}
                >
                  <span aria-hidden="true">{isSelected ? '✓ ' : ''}</span>
                  {option.label}
                </li>
              );
            })}
          </ul>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

import * as SwitchPrimitive from '@radix-ui/react-switch';
import { cn } from '../lib/cn';

interface SwitchProps {
  id?: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  /** 右侧状态文字（开启/关闭），开关本身不带业务词。 */
  label?: string;
  'aria-label'?: string;
}

/** 布尔值统一用开关表达；文案由调用方给（状态列与表单同一组件）。 */
export function Switch({
  id,
  checked,
  onCheckedChange,
  disabled,
  label,
  'aria-label': ariaLabel,
}: SwitchProps) {
  return (
    <label className={cn('inline-flex items-center gap-2 text-sm', disabled && 'opacity-60')}>
      <SwitchPrimitive.Root
        id={id}
        checked={checked}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        aria-label={ariaLabel}
        className={cn(
          'relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full',
          'border border-transparent bg-line-strong transition-colors',
          'data-[state=checked]:bg-primary',
          'disabled:cursor-not-allowed',
        )}
      >
        <SwitchPrimitive.Thumb
          className={cn(
            'block size-4 translate-x-0.5 rounded-full bg-white shadow-sm transition-transform',
            'data-[state=checked]:translate-x-[18px]',
          )}
        />
      </SwitchPrimitive.Root>
      {label ? (
        <span className={cn('select-none tabular', checked ? 'text-fg' : 'text-muted')}>{label}</span>
      ) : null}
    </label>
  );
}

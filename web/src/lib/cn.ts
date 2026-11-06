import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * 条件类名唯一入口：clsx 处理条件/数组，twMerge 处理同族冲突（后写的覆盖先写的）。
 * 约定：组件里不出现字符串拼接的 className，一律走 cn()。
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

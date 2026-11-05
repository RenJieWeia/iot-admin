import type { ReactNode } from 'react';
import { StatePanel } from './StatePanel';

interface Props {
  title: string;
  text: string;
  action?: ReactNode;
}

/** 无数据（区别于「无匹配」）：说明为什么空 + 给出第一条的录入入口。 */
export function EmptyState({ title, text, action }: Props) {
  return <StatePanel title={title} text={text} actions={action} />;
}

import { useState } from 'react';
import { updateRow } from '../api/client';
import type { RowData } from '../api/types';
import { Switch } from './Switch';
import { useToast } from './Toast';

interface Props {
  row: RowData;
  onSaved: () => void;
}

/** 终端启用开关：停用后接入面按 terminal_unknown 拒收，快照/最新值停止更新。 */
export function TerminalEnabledSwitch({ row, onSaved }: Props) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const enabled = row.enabled === true;

  const toggle = async (next: boolean) => {
    setBusy(true);
    try {
      await updateRow('terminals', String(row.id), { enabled: next });
      toast.success(next ? '已启用终端' : '已停用终端，接入面将拒收其上报');
      onSaved();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '切换失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Switch
      checked={enabled}
      disabled={busy}
      onCheckedChange={toggle}
      label={enabled ? '启用' : '停用'}
      aria-label={`${String(row.id)} 启用`}
    />
  );
}

import { useState } from 'react';
import { updateRow } from '../api/client';
import type { RowData } from '../api/types';
import { ConfirmDialog } from './ConfirmDialog';
import { Switch } from './Switch';
import { useToast } from './Toast';

interface Props {
  row: RowData;
  onSaved: () => void;
}

/** 网关启用开关：开启即在 Broker 注册（回一次性密码），关闭即注销 Broker 账号。 */
export function GatewayEnabledSwitch({ row, onSaved }: Props) {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [credential, setCredential] = useState<{ username: string; password: string } | null>(null);
  const enabled = row.enabled === true;

  const toggle = async (next: boolean) => {
    setBusy(true);
    try {
      const saved = await updateRow('gateways', String(row.gateway_id), { enabled: next });
      if (typeof saved.broker_password === 'string' && saved.broker_password) {
        // 首次启用：Broker 只回一次密码，先让操作员保存再刷新。
        setCredential({ username: String(saved.gateway_id ?? row.gateway_id), password: saved.broker_password });
        return;
      }
      toast.success(next ? '已启用，Broker 账号已开通' : '已停用，Broker 账号已注销');
      onSaved();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '切换失败');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Switch
        checked={enabled}
        disabled={busy}
        onCheckedChange={toggle}
        label={enabled ? '启用' : '停用'}
        aria-label={`${String(row.gateway_id)} 启用`}
      />
      {credential ? (
        <ConfirmDialog
          open
          danger={false}
          title="网关凭证（仅显示一次）"
          description={
            <span className="block">
              网关已在 Broker 注册，用下面的账号连接。密码只显示这一次，请立即保存。
              <span className="mt-2 block rounded-md bg-surface-2 p-2 font-mono text-xs text-fg">
                用户名：{credential.username}
                <br />
                密码：{credential.password}
              </span>
            </span>
          }
          confirmLabel="我已保存"
          cancelLabel="关闭"
          onConfirm={() => {
            setCredential(null);
            onSaved();
          }}
          onOpenChange={(open) => {
            if (!open) {
              setCredential(null);
              onSaved();
            }
          }}
        />
      ) : null}
    </>
  );
}

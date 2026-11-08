import * as Dialog from '@radix-ui/react-dialog';
import { useState } from 'react';
import type { GatewayCredential } from '../api/client';

interface CredentialDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  loading: boolean;
  error: string | null;
  credential: GatewayCredential | null;
  onReset: () => void;
}

const ROWS: { key: keyof GatewayCredential; label: string; mono?: boolean }[] = [
  { key: 'mqtt_topic', label: 'MQTT 主题', mono: true },
  { key: 'mqtt_username', label: 'MQTT 用户名', mono: true },
  { key: 'mqtt_password', label: 'MQTT 密码', mono: true },
];

/** 网关 MQTT 凭据：主题/用户名/密码，支持逐项复制；密码只在解密后展示。 */
export function CredentialDialog({ open, onOpenChange, loading, error, credential, onReset }: CredentialDialogProps) {
  const [copied, setCopied] = useState<string | null>(null);

  const copy = async (key: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(key);
      window.setTimeout(() => setCopied((current) => (current === key ? null : current)), 1200);
    } catch {
      // 剪贴板被拒时静默：值本身已展示，用户可手动选择复制。
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-[60] bg-black/40" />
        <Dialog.Content className="fixed top-1/2 left-1/2 z-[70] w-[calc(100vw-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-lg border border-line bg-surface p-5 shadow-panel outline-none">
          <Dialog.Title className="text-base font-semibold text-fg">
            MQTT 凭据{credential ? ` · ${credential.gateway_id}` : ''}
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-xs text-muted">
            网关使用这组账号连接 Broker，只允许向该主题发布报文。
          </Dialog.Description>

          {loading ? <p className="mt-4 text-sm text-muted">读取中…</p> : null}
          {error ? (
            <div className="mt-4 rounded-md border border-warning bg-warning-soft p-3 text-sm text-warning">
              {error}
            </div>
          ) : null}

          {credential ? (
            <dl className="mt-4 space-y-2">
              {ROWS.map((row) => {
                const value = String(credential[row.key] ?? '');
                return (
                  <div key={row.key} className="flex items-center gap-2 rounded-md border border-line bg-surface-2 px-3 py-2">
                    <dt className="w-24 shrink-0 text-xs text-muted">{row.label}</dt>
                    <dd className={`min-w-0 flex-1 truncate text-sm text-fg ${row.mono ? 'font-mono' : ''}`}>
                      {value || '—'}
                    </dd>
                    <button
                      type="button"
                      onClick={() => void copy(row.key, value)}
                      className="shrink-0 rounded border border-line px-2 py-1 text-xs text-fg hover:bg-surface"
                    >
                      {copied === row.key ? '已复制' : '复制'}
                    </button>
                  </div>
                );
              })}
            </dl>
          ) : null}

          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={onReset}
              disabled={loading}
              className="h-9 rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2 disabled:opacity-50"
            >
              重置密码
            </button>
            <Dialog.Close asChild>
              <button
                type="button"
                className="h-9 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover"
              >
                关闭
              </button>
            </Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

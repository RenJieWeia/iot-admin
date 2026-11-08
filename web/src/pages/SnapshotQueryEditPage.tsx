import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Loader2 } from 'lucide-react';
import { createRow, fetchList, fetchOne, fetchOptions, updateRow } from '../api/client';
import type { OptionItem, RowData } from '../api/types';
import { EmptyState, ErrorState, Skeleton } from '../components/States';
import { useToast } from '../components/Toast';
import { cn } from '../lib/cn';

interface PropertyRow extends RowData {
  type_id: number;
  item_code: string;
  name: string;
  unit?: string | null;
}

/** 模板编辑：选择终端类型与字段（档位/时间不存模板，查询时再选）。 */
export function SnapshotQueryEditPage() {
  const { id } = useParams();
  const isNew = !id;
  const navigate = useNavigate();
  const toast = useToast();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [typeOptions, setTypeOptions] = useState<OptionItem[]>([]);
  const [selectedTypes, setSelectedTypes] = useState<number[]>([]);
  const [selectedCodes, setSelectedCodes] = useState<string[]>([]);
  const [properties, setProperties] = useState<PropertyRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [types, props, template] = await Promise.all([
        fetchOptions('device-types', { limit: 1000 }),
        fetchList('entity', 'thing-model-properties', new URLSearchParams({ pageSize: '1000' })),
        isNew ? Promise.resolve(null) : fetchOne('snapshot-queries', id!),
      ]);
      setTypeOptions(types);
      setProperties(props.rows as PropertyRow[]);
      if (template) {
        setName(String(template.name ?? ''));
        setDescription(String(template.description ?? ''));
        const typeIDs = Array.isArray(template.device_type_ids)
          ? (template.device_type_ids as number[]).map(Number)
          : [];
        const codes = Array.isArray(template.item_codes)
          ? (template.item_codes as string[]).map(String)
          : [];
        setSelectedTypes(typeIDs);
        setSelectedCodes(codes);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '模板加载失败');
    } finally {
      setLoading(false);
    }
  }, [id, isNew]);

  useEffect(() => {
    void load();
  }, [load]);

  const propertiesByType = useMemo(() => {
    const map = new Map<number, PropertyRow[]>();
    for (const prop of properties) {
      const list = map.get(Number(prop.type_id)) ?? [];
      list.push(prop);
      map.set(Number(prop.type_id), list);
    }
    return map;
  }, [properties]);

  const typeName = (typeID: number) =>
    typeOptions.find((option) => option.value === String(typeID))?.label ?? String(typeID);

  const toggleType = (typeID: number, checked: boolean) => {
    setSelectedTypes((current) =>
      checked ? [...current, typeID] : current.filter((value) => value !== typeID),
    );
    // 取消类型时，把该类型下已选字段一并移除，避免保存出"无主字段"。
    if (!checked) {
      const codesOfType = new Set((propertiesByType.get(typeID) ?? []).map((p) => p.item_code));
      setSelectedCodes((current) => current.filter((code) => !codesOfType.has(code)));
    }
  };

  const toggleCode = (code: string, checked: boolean) => {
    setSelectedCodes((current) =>
      checked ? [...new Set([...current, code])] : current.filter((value) => value !== code),
    );
  };

  const save = async () => {
    if (!name.trim()) {
      toast.error('请填写模板名称');
      return;
    }
    if (selectedTypes.length === 0) {
      toast.error('至少选择一个终端类型');
      return;
    }
    if (selectedCodes.length === 0) {
      toast.error('至少选择一个字段');
      return;
    }
    setSaving(true);
    try {
      const body = {
        name: name.trim(),
        description: description.trim(),
        device_type_ids: selectedTypes,
        item_codes: selectedCodes,
      };
      const row = isNew
        ? await createRow('snapshot-queries', body)
        : await updateRow('snapshot-queries', id!, body);
      toast.success(isNew ? '已创建模板' : '已保存模板');
      if (isNew) navigate(`/e/snapshot-queries/${String(row.id)}/query`);
      else navigate('/e/snapshot-queries');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  };

  if (error) {
    return (
      <div className="mx-auto max-w-[1400px] px-6 py-5">
        <ErrorState message={error} onRetry={() => void load()} />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-[1000px] px-6 py-5">
      <Link to="/e/snapshot-queries" className="inline-flex items-center gap-1 text-xs text-muted hover:text-fg">
        <ArrowLeft className="size-3.5" aria-hidden />
        返回模板列表
      </Link>
      <div className="mt-2">
        <h1 className="text-xl font-semibold text-fg">{isNew ? '新建查询模板' : '编辑查询模板'}</h1>
        <p className="mt-1 text-xs text-muted">
          模板只决定表格的行（终端）与列（字段）；档位与时间范围在查询时选择
        </p>
      </div>

      <section className="mt-4 rounded-xl border border-line bg-surface p-4 shadow-panel">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block text-sm">
            <span className="text-xs text-muted">模板名称</span>
            <input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="例如：注塑机温度压力"
              className="mt-1 h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg focus:border-primary"
            />
          </label>
          <label className="block text-sm">
            <span className="text-xs text-muted">说明（可选）</span>
            <input
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="这条模板用来做什么"
              className="mt-1 h-9 w-full rounded-md border border-line bg-surface px-3 text-sm text-fg focus:border-primary"
            />
          </label>
        </div>
      </section>

      <section className="mt-4 rounded-xl border border-line bg-surface shadow-panel">
        <header className="border-b border-line px-4 py-3">
          <h2 className="text-sm font-medium text-fg">终端类型</h2>
          <p className="mt-0.5 text-xs text-muted">同类型终端共享字段；查询时自动展开该类型下所有终端</p>
        </header>
        {loading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-8 w-full" />
            ))}
          </div>
        ) : typeOptions.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted">还没有终端类型，请先在「终端类型」里创建。</p>
        ) : (
          <div className="grid gap-2 p-4 sm:grid-cols-2">
            {typeOptions.map((option) => {
              const typeID = Number(option.value);
              const checked = selectedTypes.includes(typeID);
              return (
                <label
                  key={option.value}
                  className={cn(
                    'flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2 text-sm',
                    checked ? 'border-primary bg-primary-soft text-fg' : 'border-line text-fg hover:bg-surface-2',
                  )}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={(event) => toggleType(typeID, event.target.checked)}
                    className="size-4 accent-[var(--primary)]"
                  />
                  <span className="min-w-0 truncate">{option.label}</span>
                </label>
              );
            })}
          </div>
        )}
      </section>

      <section className="mt-4 rounded-xl border border-line bg-surface shadow-panel">
        <header className="border-b border-line px-4 py-3">
          <h2 className="text-sm font-medium text-fg">字段</h2>
          <p className="mt-0.5 text-xs text-muted">从已选类型的物模型字段里挑；同名字段按类型分别成列</p>
        </header>
        {loading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2].map((index) => (
              <Skeleton key={index} className="h-8 w-full" />
            ))}
          </div>
        ) : selectedTypes.length === 0 ? (
          <p className="px-4 py-6 text-sm text-muted">先选择终端类型，再从该类型的字段里挑选。</p>
        ) : (
          <div className="divide-y divide-line">
            {selectedTypes.map((typeID) => {
              const props = propertiesByType.get(typeID) ?? [];
              return (
                <div key={typeID} className="px-4 py-3">
                  <p className="text-xs font-medium text-muted">{typeName(typeID)}</p>
                  {props.length === 0 ? (
                    <p className="mt-1 text-xs text-muted">该类型还没有绑定字段（可在终端类型详情里新增）。</p>
                  ) : (
                    <div className="mt-2 flex flex-wrap gap-2">
                      {props.map((prop) => {
                        const checked = selectedCodes.includes(prop.item_code);
                        return (
                          <label
                            key={`${typeID}-${prop.item_code}`}
                            className={cn(
                              'inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs',
                              checked ? 'border-primary bg-primary-soft text-primary' : 'border-line text-fg hover:bg-surface-2',
                            )}
                          >
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={(event) => toggleCode(prop.item_code, event.target.checked)}
                              className="size-3.5 accent-[var(--primary)]"
                            />
                            {prop.name}（{prop.item_code}{prop.unit ? ` / ${prop.unit}` : ''}）
                          </label>
                        );
                      })}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </section>

      <div className="mt-4 flex items-center justify-end gap-2">
        <Link
          to="/e/snapshot-queries"
          className="inline-flex h-9 items-center rounded-md border border-line px-3 text-sm text-fg hover:bg-surface-2"
        >
          取消
        </Link>
        <button
          type="button"
          onClick={() => void save()}
          disabled={saving || loading}
          className="inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-white hover:bg-primary-hover disabled:opacity-50"
        >
          {saving ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
          保存模板
        </button>
      </div>

      {selectedTypes.length === 0 && !loading ? (
        <div className="mt-6">
          <EmptyState title="还没有可预览的内容" hint="选择终端类型与字段后保存，就能在查询页看到透视表。" />
        </div>
      ) : null}
    </div>
  );
}

import { describe, expect, it } from 'vitest';
import type { FieldMeta } from '../api/types';
import { metaFixture } from '../test/fixtures';
import { formFields, isWritable } from './fieldMeta';

function entity(name: string) {
  const found = metaFixture.entities.find((item) => item.name === name);
  if (!found) throw new Error(`fixtures 缺少实体 ${name}`);
  return found;
}

describe('formFields 由 _meta.editable 驱动', () => {
  it('只返回 editable:true 的字段（自增主键被排除）', () => {
    expect(formFields(entity('terminals')).map((f) => f.key)).toEqual([
      'id',
      'type_id',
      'gateway_id',
      'name',
      'enabled',
    ]);
    // 自增主键：editable=false，即使 type=id 也不进表单。
    expect(formFields(entity('data-domains')).map((f) => f.key)).toEqual(['code', 'name']);
  });

  it('editable:true 的 json 列（非必填）可以进表单', () => {
    const fields = formFields(entity('quality-rules'));
    expect(fields.map((f) => f.key)).toEqual([
      'terminal_id',
      'item_code',
      'rule_type',
      'enabled',
      'params',
    ]);
    const params = fields.find((f) => f.key === 'params');
    expect(params).toMatchObject({ type: 'json', editable: true, required: false });
  });

  it('异常两页字段全 editable:false → 该实体不可写（不看路由）', () => {
    for (const slot of metaFixture.exceptions) {
      expect(slot.fields.every((f) => f.editable === false)).toBe(true);
      expect(isWritable(slot)).toBe(false);
      expect(formFields(slot)).toEqual([]);
    }
    expect(isWritable(entity('terminals'))).toBe(true);
    // readOnly 的实体即使有 editable 字段也不可写。
    expect(isWritable({ ...entity('terminals'), readOnly: true })).toBe(false);
    // 没有 editable 字段的可写实体同样不可写（fail-closed）。
    const noEditable = {
      ...entity('terminals'),
      fields: entity('terminals').fields.map((f) => ({ ...f, editable: false })),
    };
    expect(isWritable(noEditable)).toBe(false);
  });

  it('fail-closed：契约缺 editable 键时视为不可编辑，不回退到 id && filterable 推断', () => {
    const legacy = { ...(entity('terminals').fields[0] as FieldMeta) };
    delete (legacy as Partial<FieldMeta>).editable;
    expect(formFields({ fields: [legacy] })).toEqual([]);
  });
});

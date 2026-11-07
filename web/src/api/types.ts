// 字段元数据与列表契约的 TS 映射，逐字对应 task-004 §4（R-019）。
// 唯一事实源是 internal/admin/fields.go；前端不得硬编码列清单、业务枚举中文或列宽策略。
export type Tone = 'neutral' | 'positive' | 'negative' | 'warning';

export type FieldType =
  | 'text'
  | 'id'
  | 'enum'
  | 'number'
  | 'boolean'
  | 'relation'
  | 'json'
  | 'datetime';

export type FilterType =
  | 'text'
  | 'multiSelect'
  | 'numberRange'
  | 'dateRange'
  | 'boolean'
  | 'relation'
  | 'remoteSelect'
  | 'none';

export type FilterOp = 'contains' | 'eq' | 'prefix' | 'in' | 'gte' | 'lte';

export type WidthStrategy = 'fixed' | 'min' | 'flex';

export interface Option {
  value: string;
  label: string;
  /** 状态标签颜色语义；后端未下发时前端按 neutral 渲染（文字始终保留）。 */
  tone?: Tone;
}

/** 关联字段的目标（GET /admin/v1/options/{entity} 的取值与展示列）。 */
export interface FieldRef {
  entity: string;
  valueKey: string;
  labelKey: string;
}

export interface FieldMeta {
  key: string;
  label: string;
  type: FieldType;
  visible: boolean;
  sortable: boolean;
  filterable: boolean;
  filterType: FilterType;
  operators: FilterOp[];
  /** 操作符 → 中文（后端下发，前端不再自维护一份）。 */
  operatorLabels: Record<string, string>;
  optionsSource?: 'dict' | 'remote' | 'static';
  /** 永远是数组（后端把 nil 归一成 []），空数组表示无枚举。 */
  options: Option[];
  advancedOnly?: boolean;
  /** 是否可写进抽屉表单（后端按 admin.Entity.Cols 下发）；缺省 false，前端不做推断。 */
  editable: boolean;
  /** 是否必填（后端下发）；缺省 false，缺省时前端不阻止空提交。 */
  required: boolean;
  widthStrategy: WidthStrategy;
  /**
   * 列宽（px）的**后端推导结论**：字段显式覆写优先，否则按字段类型推导。
   * 前端只消费这个值，不自行按类型重算；缺省（json/未知类型）时回落到 widthStrategy 的 CSS 类。
   */
  width?: number;
  ref?: FieldRef;
  /** ref.entity 的兼容别名（R-019 首版键名）；新代码只用 ref。 */
  relation?: string;
}

/** 可写实体（6 个，进 admin.Entities）。 */
export interface EntityMeta {
  /** URL 段，例如 terminals。 */
  name: string;
  /** 导航与页面标题。 */
  title: string;
  /** 后端列表路由（/admin/v1/<name>），仅作展示与排查用途。 */
  route: string;
  /** 只读实体不渲染任何写入口。 */
  readOnly: boolean;
  /** 服务端默认排序与方向，前端不再自行猜测。 */
  defaultSort: string;
  defaultOrder: 'asc' | 'desc';
  fields: FieldMeta[];
}

/**
 * 通用只读页（task-009 §2 A3：`_meta.readonly`）。
 * `exceptions` 是它的特例，本期保留独立键，前端不改其用法。
 */
export interface ReadonlyMeta {
  /** URL 段，例如 latest-values。 */
  slot: string;
  name: string;
  title: string;
  route: string;
  readOnly: boolean;
  defaultSort: string;
  defaultOrder: 'asc' | 'desc';
  /** 分页形态由后端下发：offset 用页码 + 总数，cursor 用「加载更多」（无 total）。 */
  pagination?: 'offset' | 'cursor';
  fields: FieldMeta[];
}

/** 只读异常页（1 个：events，独立路由，不进 admin.Entities）。 */
export interface ExceptionMeta {
  slot: 'events';
  /** 对应的内部实体名（exception-events）。 */
  name: string;
  title: string;
  route: string;
  readOnly: boolean;
  defaultSort: string;
  defaultOrder: 'asc' | 'desc';
  fields: FieldMeta[];
}

/** 分页档位由后端下发（默认 10、上限 50）。 */
export interface PaginationMeta {
  default: number;
  max: number;
  options: number[];
}

export interface MetaPayload {
  entities: EntityMeta[];
  exceptions: ExceptionMeta[];
  /** 通用只读页；后端未下发时按空数组处理（fail-closed：不出现多余导航）。 */
  readonly?: ReadonlyMeta[];
  /**
   * 子资源：形状与 entities 相同，装「可 CRUD 但不出现在导航」的实体
   * （当前只有终端属性 thing-model-properties），供详情页做字段元数据。
   */
  subresources?: EntityMeta[];
  /** 枚举字典：字段名 → { 取值: 中文 }。 */
  enums: Record<string, Record<string, string>>;
  /** 布尔字典：{ "true": "是", "false": "否" }。 */
  booleans: Record<string, string>;
  pagination: PaginationMeta;
}

export interface OptionItem {
  value: string;
  label: string;
}

export interface ListMeta {
  total: number;
  page: number;
  pageSize: number;
  sort: string;
  order: 'asc' | 'desc';
}

export interface ListPayload {
  data: RowData[];
  meta: ListMeta;
}


export type RowData = Record<string, unknown>;

/** 游标分页的 meta：不提供 total，只用 hasMore/nextCursor 判断是否还能继续。 */
export interface CursorMeta {
  hasMore: boolean;
  nextCursor: string | null;
  limit: number;
}

export interface Envelope<T> {
  code: number;
  message: string;
  data?: T;
  meta?: ListMeta;
}

/** 关联字段取值：value → label。 */
export type RelationOptions = Record<string, string>;

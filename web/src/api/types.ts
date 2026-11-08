// 与 internal/admin/meta.go 的 _meta 契约逐字对应（唯一事实源在 Go 侧）。
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
  tone?: Tone;
}

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
  operatorLabels: Record<string, string>;
  optionsSource: string;
  options: Option[];
  advancedOnly?: boolean;
  derived?: boolean;
  widthStrategy: WidthStrategy;
  width?: number;
  editable: boolean;
  required: boolean;
  ref?: FieldRef;
  relation?: string;
}

export interface PageMeta {
  name: string;
  title: string;
  route: string;
  readOnly: boolean;
  defaultSort: string;
  defaultOrder: 'asc' | 'desc';
  fields: FieldMeta[];
}

export interface EntityMeta extends PageMeta {}

export interface ReadonlyMeta extends PageMeta {
  slot: string;
  pagination?: 'offset' | 'cursor';
}

export interface PaginationMeta {
  default: number;
  max: number;
  options: number[];
}

export interface MetaPayload {
  entities: EntityMeta[];
  subresources?: EntityMeta[];
  exceptions: ReadonlyMeta[];
  readonly?: ReadonlyMeta[];
  enums: Record<string, Record<string, string>>;
  booleans: Record<string, string>;
  pagination: PaginationMeta;
}

export type RowData = Record<string, unknown>;

export interface ListMeta {
  total: number;
  page: number;
  pageSize: number;
  sort: string;
  order: 'asc' | 'desc';
}

export interface CursorMeta {
  hasMore: boolean;
  nextCursor?: string | null;
  limit: number;
}

export interface Envelope<T> {
  code: number;
  message: string;
  data?: T;
  meta?: ListMeta & Partial<CursorMeta>;
}

export interface OptionItem {
  value: string;
  label: string;
}

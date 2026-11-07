/**
 * 表格视觉的唯一来源：DataTable 与页面内的自建表格（如绑定列表）共用同一套类。
 *
 * 这样「行高、行线、表头、hover、sticky 投影、操作列形态」只有一份定义，
 * 页面不会再各写一套导致两张表长得不一样。
 *
 * 约束：所有值都来自令牌或 tailwind.css 的 @utility，TSX 里不出现 arbitrary value。
 */

/** 表格容器：横向滚动 + 圆角边框（与 @utility table-shell 同源）。 */
export const TABLE_WRAPPER = 'table-shell';

/** 表头单元格：正文色 + 加粗一档，底色 surface-2 保持可辨。 */
export const TH_BASE =
  'h-11 whitespace-nowrap border-b border-border bg-surface-2 px-3 text-left text-sm font-semibold text-text';

/**
 * 首列表头：sticky 定位 + 投影锚点。
 * 类名在此只作选择器锚点，box-shadow 定义见 global.css 的 [data-scroll-*] 规则
 * （只有发生横向滚动时才显示投影）。
 */
export const TH_STICKY_LEFT = 'sticky left-0 z-20 sticky-shadow-left';

/**
 * 操作列表头：右侧 sticky + 投影锚点。
 * 类名在此只作选择器锚点，box-shadow 定义见 global.css 的 [data-scroll-*] 规则。
 */
export const TH_STICKY_RIGHT = 'sticky right-0 z-20 text-right sticky-shadow-right';

/** 数据单元格：行线只用最浅一档，保证 1px 且不抢内容。 */
export const TD_BASE = 'h-11 overflow-hidden border-b border-border-subtle px-3 align-middle';

/** 首列数据单元格：sticky + 底色随 hover 变化；投影见 global.css 的 [data-scroll-*] 规则。 */
export const TD_STICKY_LEFT = 'sticky left-0 z-10 bg-surface group-hover:bg-surface-2 sticky-shadow-left';

/** 操作列数据单元格：右侧 sticky + 底色随 hover 变化；投影见 global.css 的 [data-scroll-*] 规则。 */
export const TD_STICKY_RIGHT =
  'sticky right-0 z-10 whitespace-nowrap bg-surface text-right group-hover:bg-surface-2 sticky-shadow-right';

/** 行：hover 提亮一档（表格行统一 hover）。 */
export const TR_ROW = 'group hover:bg-surface-2';

/**
 * 操作列文字链（主操作）。
 *
 * 必须显式 `border-0 bg-transparent cursor-pointer`：本仓**没有引入 Tailwind preflight**
 * （见 tailwind.css 顶部注释），`<button>` 会带 UA 默认样式（2px outset 黑边 + #efefef 底色），
 * 不加这几项看起来就是个描边按钮，与「文字链」的设计不一致。
 */
export const ACTION_LINK =
  'inline-flex h-8 cursor-pointer items-center rounded-sm border-0 bg-transparent px-2 text-base text-text hover:bg-surface-2';

/** 操作列文字链（危险操作）：默认 muted，hover 转危险色。 */
export const ACTION_DANGER =
  'inline-flex h-8 cursor-pointer items-center rounded-sm border-0 bg-transparent px-2 text-base text-muted hover:bg-danger-weak hover:text-danger';

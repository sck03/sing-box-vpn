import { el, card, help, button, resourceEditor } from "./ui.js";

export const processorFields = [
  {
    key: "processor.protocols",
    label: "保留协议",
    type: "csv",
    section: "节点筛选",
  },
  { key: "processor.tags", label: "保留标签", type: "csv" },
  { key: "processor.include", label: "名称包含（正则）" },
  { key: "processor.exclude", label: "名称排除（正则）" },
  { key: "processor.prefix", label: "名称前缀", section: "重命名与整理" },
  { key: "processor.rename_pattern", label: "重命名匹配（正则）" },
  {
    key: "processor.rename_replacement",
    label: "替换为",
    help: "捕获组可使用 ${1}、${2}。",
  },
  { key: "processor.deduplicate", label: "按连接参数去重", type: "bool" },
  { key: "processor.sort", label: "按名称排序", type: "bool" },
];

let selectedTemplate;
export function templateResources(templates, catalog, change) {
  const eligible = templates.filter(
    (t) => t.mode === "config" && catalog[t.client]?.features.policy_groups,
  );
  if (!eligible.length)
    return help(
      "创建支持策略组的完整配置模板后，可编辑策略组和规则 Provider。",
    );
  const select = el("select", { "aria-label": "编辑模板资源" });
  for (const t of eligible)
    select.append(
      el("option", { value: t.id }, t.id + " · " + catalog[t.client].label),
    );
  if (eligible.some((t) => t.id === selectedTemplate))
    select.value = selectedTemplate;
  const body = el("div", { class: "template-resource-grid" });
  function render() {
    selectedTemplate = select.value;
    const t = eligible.find((t) => t.id === select.value);
    const groups = t.groups || [],
      providers = t.providers || [],
      rules = t.rules || [];
    const save = () => {
      t.groups = groups;
      t.providers = providers;
      t.rules = rules;
      change();
    };
    const targets = [
      "PROXY",
      "VPNGate",
      "DIRECT",
      "REJECT",
      ...groups.map((g) => g.id),
    ];
    body.replaceChildren(
      resourceEditor({
        title: "策略组",
        items: groups,
        change: save,
        fields: [
          { key: "id", label: "组名称", required: true },
          {
            key: "type",
            label: "策略",
            type: "select",
            options:
              t.client === "sing-box"
                ? ["select", "urltest"]
                : ["select", "urltest", "fallback"],
          },
          {
            key: "scope",
            label: "节点范围",
            type: "select",
            options: [
              ["normal", "普通出口"],
              ["vpngate", "VPNGate"],
              ["", "全部"],
            ],
          },
          { key: "include", label: "名称筛选（正则）" },
          {
            key: "members",
            label: "引用其他策略组",
            type: "csv",
            help: "填写后使用这些组作为成员；服务端会检查循环引用。",
          },
          {
            key: "url",
            label: "测速 URL",
            visible: (v) => v.type !== "select",
          },
          {
            key: "interval_seconds",
            label: "检测间隔（秒）",
            type: "number",
            min: 30,
            max: 86400,
            visible: (v) => v.type !== "select",
          },
          {
            key: "tolerance",
            label: "测速容差（毫秒）",
            type: "number",
            min: 0,
            visible: (v) => v.type === "urltest",
          },
        ],
        defaults: {
          id: "",
          type: "urltest",
          scope: "normal",
          members: [],
          url: "https://www.gstatic.com/generate_204",
          interval_seconds: 300,
          tolerance: 50,
        },
        columns: [
          { key: "id", label: "组名称" },
          { key: "type", label: "策略" },
          { key: "scope", label: "范围" },
        ],
      }),
      resourceEditor({
        title: "远程规则 Provider",
        items: providers,
        change: save,
        fields: [
          { key: "id", label: "ID", required: true },
          {
            key: "url",
            label: "规则 URL",
            type: "url",
            required: true,
            wide: true,
          },
          {
            key: "type",
            label: "规则类型",
            type: "select",
            options: ["domain", "ip_cidr", "classical"],
          },
          {
            key: "format",
            label: "格式",
            type: "select",
            options:
              t.client === "sing-box" ? ["srs", "json"] : ["text", "mrs"],
          },
          {
            key: "target",
            label: "目标策略",
            type: "select",
            options: targets,
          },
        ],
        defaults: {
          id: "",
          url: "",
          type: "domain",
          format: t.client === "sing-box" ? "srs" : "mrs",
          target: "PROXY",
        },
        columns: [
          { key: "id", label: "ID" },
          { key: "format", label: "格式" },
          { key: "target", label: "目标" },
        ],
      }),
      resourceEditor({
        title: "本地有序规则",
        items: rules,
        change: save,
        extraActions: (rule) =>
          [-1, 1].map((step) =>
            button(step < 0 ? "↑" : "↓", () => {
              const index = rules.indexOf(rule),
                next = index + step;
              if (next < 0 || next >= rules.length) return;
              [rules[index], rules[next]] = [rules[next], rules[index]];
              save();
            }),
          ),
        fields: [
          {
            key: "type",
            label: "匹配",
            type: "select",
            options: ["domain", "domain_suffix", "ip_cidr"],
          },
          { key: "value", label: "域名或 CIDR", required: true },
          {
            key: "target",
            label: "目标策略",
            type: "select",
            options: targets,
          },
        ],
        defaults: { type: "domain_suffix", value: "", target: "PROXY" },
        columns: [
          { key: "type", label: "匹配" },
          { key: "value", label: "值" },
          { key: "target", label: "目标" },
        ],
      }),
    );
  }
  select.addEventListener("change", render);
  render();
  return card(
    "模板策略与规则",
    help(
      "本地规则优先于自定义 Provider，国内分流规则随后执行。策略组可以按区域或服务筛选节点。",
    ),
    select,
    body,
  );
}

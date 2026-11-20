// Shared labels and binding rules for panel, subscription, inbound and VPN TLS.
export const certificateSources = {
  acme: "自动证书",
  import: "导入证书",
  generated: "自签名（仅测试）",
};
const canonical = (value = "") => value.replaceAll("\\", "/");
export function boundCertificate(options, certificates) {
  return (
    certificates.find(
      (asset) =>
        canonical(asset.certificate) === canonical(options.certificate) &&
        canonical(asset.key) === canonical(options.key),
    )?.id || ""
  );
}
export function certificateChoices(certificates) {
  return certificates.map((asset) => [
    asset.id,
    [
      asset.domain,
      certificateSources[asset.provider] || asset.provider,
      asset.status?.error
        ? "不可用 / 待申请"
        : asset.status?.days_left != null
          ? "剩余 " + asset.status.days_left + " 天"
          : "待检查",
    ].join(" · "),
  ]);
}
export function withCertificateStatus(certificates, status) {
  const byID = new Map(status.map((item) => [item.id, item]));
  return certificates.map((asset) => ({
    ...asset,
    status: byID.get(asset.id),
  }));
}

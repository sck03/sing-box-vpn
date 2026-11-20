export function supportsPlatform(profile, platform) {
  return !platform || !profile.platforms?.length ||
    profile.platforms.includes(platform) || profile.platforms.includes("通用导入");
}

export function platformChoices(profiles) {
  return [...new Set(profiles.flatMap((profile) => profile.platforms || []))]
    .filter((name) => name !== "通用导入").sort();
}

export function applicationImport(profile, address) {
  // Keep the public page safe even if a server returns unexpected metadata.
  if (profile.client === "stash" && profile.mode === "config" &&
      profile.import_prefix === "stash://install-config?url=")
    return profile.import_prefix + encodeURIComponent(address);
  return "";
}

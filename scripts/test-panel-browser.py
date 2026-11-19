"""Physical input regressions for a disposable, initialized loopback panel."""

import os
import uuid
import time
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright


def check(browser, base):
    account = "browser-" + uuid.uuid4().hex[:12]
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()
    browser_errors = []
    page.on("pageerror", lambda error: browser_errors.append(str(error)))
    page.goto(base + "/")
    page.locator('[name="username"]').fill(os.getenv("PANEL_TEST_USERNAME", "reviewadmin"))
    page.locator('[name="password"]').fill(os.environ["PANEL_TEST_PASSWORD"])
    with page.expect_response(lambda r: r.url.endswith("/api/auth/login")) as login:
        page.locator("#login-form button").click()
    headers = {"X-CSRF-Token": login.value.json()["csrf"]}
    api = context.request
    services = api.get(base + "/api/overview").json()["services"]
    assert services and all(s.get("state") == "unavailable" for s in services)
    result = api.post(base + "/api/accounts", headers=headers, data={"account": {
        "id": account, "name": account, "duration_seconds": 86400, "quota_bytes": None,
    }})
    assert result.ok, result.text()
    page.goto(base + "/#accounts")
    row = page.locator("tr").filter(has_text=account)
    toggle = row.locator("summary")
    menu = row.locator("details.action-menu")
    toggle.click()
    page.locator("#page-title").click()
    expect(menu).not_to_have_attribute("open", "")
    toggle.focus()
    page.keyboard.press("Enter")
    expect(menu).to_have_attribute("open", "")
    page.keyboard.press("Escape")
    expect(toggle).to_be_focused()
    expect(menu).not_to_have_attribute("open", "")
    toggle.click()
    row.get_by_role("button", name="编辑", exact=True).click()
    dialog = page.locator("#dialog")
    expect(dialog.get_by_role("heading", name="编辑权益")).to_be_visible()
    dialog.get_by_label("名称", exact=True).fill(account + "-renamed")
    dialog.get_by_role("button", name="保存", exact=True).click()
    expect(dialog).not_to_be_visible()
    record = api.get(base + "/api/accounts?q=" + account).json()["items"][0]
    assert record["name"].endswith("-renamed")
    assert record["expires_at"] is None and record["effective_expires_at"]
    assert record["download_denial"] == "no_bound_inbound"
    assert api.post(base + "/api/grants", headers=headers, data={
        "id": account + "-unbound", "account_id": account, "clients": ["uri"],
    }).status == 400
    inbound = next(n for n in api.get(base + "/api/inbound-options?scope=normal").json() if "vmess" in n["id"])
    task = api.post(base + "/api/tasks", headers=headers, data={"operation": "account", "account": {
        "operation": "bind", "id": account, "scope": "normal", "inbound_ids": [inbound["id"]],
    }}).json()["task_id"]
    deadline = time.monotonic() + 90
    while True:
        state = api.get(base + "/api/tasks/detail?id=" + str(task)).json()["state"]
        if state not in {"queued", "running"}:
            assert state == "succeeded", state
            break
        assert time.monotonic() < deadline
        time.sleep(0.3)
    # The live API suite configures a shareable public origin. Open its token
    # on this loopback listener, keeping the whole test on the disposable host.
    grant = api.post(base + "/api/grants", headers=headers, data={
        "id": account + "-share", "account_id": account, "clients": ["uri"],
    })
    assert grant.ok, grant.text()
    page.goto(base + "/user#" + grant.json()["token"])
    expect(page.locator(".client-grid .client-download")).to_have_count(1)
    expect(page.get_by_role("heading", name="通用 URI", exact=True)).to_be_visible()
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    screenshots = os.getenv("PANEL_TEST_SCREENSHOT_DIR")
    if screenshots:
        os.makedirs(screenshots, exist_ok=True)
        page.screenshot(path=os.path.join(screenshots, "user-available.png"), full_page=True)
    page.goto(base + "/#subscriptions/status")
    expect(page.get_by_role("heading", name="已发布输出", exact=True)).to_be_visible()
    expect(page.get_by_text("已发布", exact=True)).to_be_visible()
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "subscription-status.png"), full_page=True)
    record = api.get(base + "/api/accounts?q=" + account).json()["items"][0]
    assert api.post(base + "/api/accounts", headers=headers, data={"account": {
        "id": account, "name": record["name"], "duration_seconds": 86400,
        "quota_bytes": 0, "revision": record["revision"],
    }}).ok
    page.goto(base + "/user#" + grant.json()["token"])
    expect(page.get_by_text("本期流量额度已耗尽", exact=False)).to_be_visible()
    expect(page.locator(".download-link")).to_have_count(0)
    mobile = browser.new_context(viewport={"width": 390, "height": 844}, has_touch=True,
                                 is_mobile=True, storage_state=context.storage_state())
    phone = mobile.new_page()
    phone.goto(base + "/#accounts")
    records = phone.locator("tr").filter(has_text=account)
    expect(records).to_be_visible()
    assert phone.locator(".workflow").is_hidden()
    assert phone.evaluate("document.documentElement.scrollWidth <= innerWidth")
    assert phone.locator(".remote-table tbody tr").first.bounding_box()["y"] < 844
    screenshots = os.getenv("PANEL_TEST_SCREENSHOT_DIR")
    if screenshots:
        os.makedirs(screenshots, exist_ok=True)
        phone.screenshot(path=os.path.join(screenshots, "mobile.png"), full_page=True)
        page.screenshot(path=os.path.join(screenshots, "user.png"), full_page=True)
    records.locator("summary").tap()
    records.get_by_role("button", name="编辑", exact=True).tap()
    expect(phone.locator("#dialog")).to_be_visible()
    mobile.close()
    assert not browser_errors, browser_errors
    # Service routes select different VPNGate workspaces; fixed lists have no pager.
    page.goto(base + "/#overview")
    expect(page.locator('a[href="#vpngate/access"]')).to_be_visible()
    expect(page.locator('a[href="#vpngate/gateway"]')).to_be_visible()
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "services.png"), full_page=True)
    page.goto(base + "/#access/panel")
    page.get_by_role("button", name="配置面板访问", exact=True).click()
    expect(page.get_by_label("访问方式", exact=True)).to_have_count(1)
    assert page.get_by_label("访问方式", exact=True).locator("option").count() == 3
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "public-access.png"), full_page=True)
    page.keyboard.press("Escape")
    page.goto(base + "/#access/subscription")
    page.get_by_role("button", name="配置独立下载监听", exact=True).click()
    assert page.get_by_label("访问方式", exact=True).locator("option").count() == 2
    page.keyboard.press("Escape")
    page.goto(base + "/#maintenance/logs")
    expect(page.get_by_role("button", name="保留策略", exact=True)).to_be_visible()
    expect(page.get_by_role("button", name="清理运行日志", exact=True)).to_be_visible()
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "maintenance.png"), full_page=True)
    page.goto(base + "/#subscriptions/templates")
    page.get_by_role("button", name="从预设添加", exact=True).click()
    page.get_by_label("客户端", exact=True).select_option("mihomo")
    page.get_by_label("用途", exact=True).select_option("automatic")
    page.get_by_label("模板名称（可留空）", exact=True).fill("自动线路")
    page.locator("#dialog").get_by_role("button", name="保存", exact=True).click()
    expect(page.locator("#dialog")).not_to_be_visible()
    expect(page.get_by_text("自动线路", exact=True)).to_be_visible()
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "templates.png"), full_page=True)
    page.get_by_role("button", name="快速创建订阅", exact=True).click()
    page.get_by_label("订阅名称", exact=True).fill("导入演示")
    page.get_by_label("来源", exact=True).select_option("inline")
    page.get_by_label("节点内容", exact=True).fill("trojan://test-password@127.0.0.1:14443?sni=localhost&insecure=1#sample")
    page.get_by_label("客户端", exact=True).select_option("mihomo")
    page.locator("#dialog").get_by_role("button", name="保存", exact=True).click()
    expect(page.get_by_role("heading", name="配置校验与发布预览", exact=True)).to_be_visible(timeout=30000)
    if screenshots:
        page.screenshot(path=os.path.join(screenshots, "subscription-preview.png"), full_page=True)
    page.keyboard.press("Escape")
    assert not browser_errors, browser_errors
    context.close()


if __name__ == "__main__":
    base = os.environ["PANEL_TEST_URL"].rstrip("/")
    if urlparse(base).hostname not in {"127.0.0.1", "localhost", "::1"}:
        raise ValueError("use a disposable loopback panel")
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(executable_path=os.getenv("CHROME_BINARY"))
        check(browser, base)
        browser.close()
    print("Browser passed: pointer, keyboard, outside dismissal, touch, stored expiry, quota message and mobile layout.")

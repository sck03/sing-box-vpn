"""Physical input regressions for a disposable, initialized loopback panel."""

import os
import uuid
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright


def check(browser, base):
    account = "browser-" + uuid.uuid4().hex[:12]
    context = browser.new_context(viewport={"width": 1280, "height": 900})
    page = context.new_page()
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
        "id": account, "name": account, "duration_seconds": 86400, "quota_bytes": 0,
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
    # The live API suite configures a shareable public origin. Open its token
    # on this loopback listener, keeping the whole test on the disposable host.
    grant = api.post(base + "/api/grants", headers=headers, data={
        "id": account + "-share", "account_id": account, "clients": ["uri"],
    })
    assert grant.ok, grant.text()
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
    records.locator("summary").tap()
    records.get_by_role("button", name="编辑", exact=True).tap()
    expect(phone.locator("#dialog")).to_be_visible()
    mobile.close()
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

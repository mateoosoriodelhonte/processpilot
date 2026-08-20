const { test, expect } = require("@playwright/test");
const AxeBuilder = require("@axe-core/playwright").default;

test.beforeEach(async ({ page }) => {
  page.unexpectedRemoteRequests = [];
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.hostname !== "127.0.0.1") {
      page.unexpectedRemoteRequests.push(request.url());
    }
  });
});

test.afterEach(async ({ page }) => {
  expect(page.unexpectedRemoteRequests).toEqual([]);
});

test("overview is useful, semantic, live, and unmistakably demo data", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1, name: "Why is my Mac slow?" })).toBeVisible();
  await expect(page.locator(".demo-banner")).toContainText("Demo data");
  await expect(page.getByText("30.4 GiB")).toBeVisible();
  await expect(page.locator("main")).toBeVisible();
  await expect(page.locator("button")).toHaveCount(0);
  await expect(page.locator("[data-live-status]")).toContainText("local only");
  await expect(page.locator("[data-updated-at]")).not.toHaveText("");
});

test("all primary pages navigate and preserve the observation boundary", async ({ page }) => {
  await page.goto("/");
  const pages = [
    ["Applications", "Applications"],
    ["Processes", "Raw processes"],
    ["History", "Resource history"],
    ["Anomalies", "Anomalies"],
    ["Privacy", "Privacy"],
    ["Settings", "Local settings"]
  ];
  for (const [link, heading] of pages) {
    await page.getByRole("link", { name: link, exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: heading })).toBeVisible();
    await expect(page.getByText("Demo data", { exact: true })).toBeVisible();
  }
  await expect(page.getByText("127.0.0.1", { exact: true })).toBeVisible();
  await expect(page.getByText("No AI", { exact: true })).toBeVisible();
});

test("process detail keeps observation, classification, and explanation separate", async ({ page }) => {
  await page.goto("/processes/95707");
  const observed = page.getByRole("heading", { level: 2, name: "Observed" });
  const classified = page.getByRole("heading", { level: 2, name: "ProcessPilot classification" });
  const explained = page.getByRole("heading", { level: 2, name: "Explanation" });
  await expect(observed).toBeVisible();
  await expect(classified).toBeVisible();
  await expect(explained).toBeVisible();
  await expect(page.locator(".detail-flow > section h2")).toHaveText(["Observed", "ProcessPilot classification", "Explanation"]);
  await expect(page.getByText("Explanation text has no authority")).toBeVisible();
  await expect(page.locator("button")).toHaveCount(0);
});

test("application detail drills into grouped evidence without adding control", async ({ page }) => {
  await page.goto("/applications");
  await page.getByRole("link", { name: /Ollama/ }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Ollama" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Resource totals" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Recent samples" })).toBeVisible();
  await expect(page.locator("button")).toHaveCount(0);

  await page.goto("/applications");
  await page.getByRole("link", { name: /Visual Studio Code/ }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Visual Studio Code" })).toBeVisible();
});

test("privacy page names concrete allowed and prohibited access", async ({ page }) => {
  await page.goto("/privacy");
  await expect(page.getByRole("heading", { level: 2, name: "What ProcessPilot can see" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "What ProcessPilot cannot see" })).toBeVisible();
  for (const phrase of ["CPU and RAM usage", "Passwords", "Browser history", "RAM contents", "Keystrokes", "Microphone", "Network packet contents"]) {
    await expect(page.getByText(new RegExp(phrase, "i"))).toBeVisible();
  }
  await expect(page.getByText(/does not require administrator privileges/i)).toBeVisible();
});

test("primary pages have no automated WCAG 2.1 AA violations", async ({ page }) => {
  for (const path of ["/", "/applications", "/applications/known%3AOllama", "/processes", "/history", "/anomalies", "/privacy", "/settings", "/processes/95707"]) {
    await page.goto(path);
    const results = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    expect(results.violations, `accessibility violations on ${path}`).toEqual([]);
  }
});

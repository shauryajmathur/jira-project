const refreshEvery = 300;
const initialParams = new URLSearchParams(window.location.search);
const state = {
  report: null,
  activities: [],
  selectedSpace: initialParams.get("space") || "",
  startDate: initialParams.get("start") || "",
  endDate: initialParams.get("end") || "",
  refreshedAt: 0,
  refreshing: false,
  pendingReload: false,
  agentReady: false,
  providers: [],
  selectedProvider: "",
  customJobID: "",
  customPoll: 0,
  selectedActivityCount: null,
};

const byId = (id) => document.getElementById(id);

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function replaceChildren(id, children) {
  byId(id).replaceChildren(...children);
}

function formatHours(value, signed = false) {
  const prefix = signed && value > 0 ? "+" : "";
  return `${prefix}${value.toFixed(1)}h`;
}

function formatPercent(value) {
  return `${value.toFixed(1)}%`;
}

function formatBytes(value) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}

function countLabel(count, noun) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

function activityLabel(count) {
  return `${count} ${count === 1 ? "activity" : "activities"}`;
}

function peopleLabel(count) {
  return `${count} ${count === 1 ? "person" : "people"}`;
}

function formatDate(dateString) {
  const date = new Date(`${dateString}T00:00:00`);
  return new Intl.DateTimeFormat(undefined, {
    day: "2-digit",
    month: "short",
    year: "numeric",
  }).format(date);
}

function categoryClass(category) {
  return category.toLowerCase().replaceAll(" ", "-");
}

function contributorWorkType(category) {
  if (category === "Story" || category === "Bug") return category;
  return "Other";
}

function contributorTypeSummary(activities, totalHours, selectedType) {
  const shown = activities.filter((activity) => selectedType === "All" || contributorWorkType(activity.category) === selectedType);
  const hours = shown.reduce((total, activity) => total + activity.actualHours, 0);
  return {
    shown,
    hours,
    share: totalHours > 0 ? hours / totalHours * 100 : 0,
  };
}

function activityBuckets(people) {
  const groups = new Map();
  for (const person of people) {
    const count = person.activityCount;
    if (!groups.has(count)) groups.set(count, []);
    groups.get(count).push(person);
  }
  return Array.from(groups, ([count, people]) => ({
    count,
    people: people.sort((a, b) => a.name.localeCompare(b.name)),
  })).sort((a, b) => a.count - b.count);
}

function showMessage(text, isError = false) {
  const message = byId("message");
  message.hidden = !text;
  message.textContent = text;
  message.className = `message${isError ? " error" : ""}`;
}

function syncURL() {
  const url = new URL(window.location.href);
  if (state.selectedSpace) url.searchParams.set("space", state.selectedSpace);
  else url.searchParams.delete("space");
  if (state.startDate) url.searchParams.set("start", state.startDate);
  else url.searchParams.delete("start");
  if (state.endDate) url.searchParams.set("end", state.endDate);
  else url.searchParams.delete("end");
  window.history.replaceState(null, "", url);

  const reportParams = new URLSearchParams();
  if (state.selectedSpace) reportParams.set("space", state.selectedSpace);
  if (state.startDate) reportParams.set("start", state.startDate);
  if (state.endDate) reportParams.set("end", state.endDate);
  const query = reportParams.toString();
  byId("download-pdf").href = `/report.pdf${query ? `?${query}` : ""}`;
}

function setLoading(loading) {
  state.refreshing = loading;
  const button = byId("refresh-button");
  button.disabled = loading;
  button.classList.toggle("is-loading", loading);
  button.lastChild.textContent = loading ? " Refreshing" : " Refresh Jira";
  byId("apply-period").disabled = loading;
  byId("period-start").disabled = loading;
  byId("period-end").disabled = loading;
}

function renderSummary(report) {
  const summary = report.summary;
  const scope = report.selectedSpace || "All spaces";
  byId("scope").textContent = scope;
  byId("period").textContent = `${formatDate(report.period.start)} – ${formatDate(report.period.end)} · ${report.period.businessDays} business days`;
  byId("last-updated").textContent = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(report.updatedAt));
  byId("sprint-hours").textContent = formatHours(summary.sprintUtilizationHours);
  const sprintDetail = [`Recorded across ${countLabel(summary.workedIssueCount, "worked issue")}`];
  if (summary.unattributedHours > 0) sprintDetail.push(`${formatHours(summary.unattributedHours)} without an author`);
  byId("sprint-detail").textContent = sprintDetail.join(" · ");
  byId("planned-hours").textContent = formatHours(summary.plannedHours);
  const variance = byId("variance-detail");
  const varianceDetail = [`${formatHours(summary.varianceHours, true)} against plan`];
  if (summary.unassignedPlannedHours > 0) varianceDetail.push(`${formatHours(summary.unassignedPlannedHours)} unassigned`);
  variance.textContent = varianceDetail.join(" · ");
  variance.className = `metric-detail${summary.varianceHours > 0 ? " detail-alert" : ""}`;
  byId("resources-with-time").textContent = `${summary.resourcesWithTime}`;
  const withoutTime = Math.max(0, summary.trackedResources - summary.resourcesWithTime);
  byId("resource-detail").textContent = withoutTime
    ? `${countLabel(summary.trackedResources, "tracked resource")} · ${withoutTime} with no logged time`
    : `${countLabel(summary.trackedResources, "tracked resource")} represented`;
  byId("issues-worked").textContent = `${summary.workedIssueCount} / ${summary.issueCount}`;
  byId("issues-detail").textContent = "Worked issues / selected sprint issues";
  byId("custom-context").textContent = `${scope} · ${formatDate(report.period.start)} – ${formatDate(report.period.end)}`;
}

async function loadCustomCapability() {
  const generate = byId("generate-custom");
  generate.disabled = true;
  try {
    const response = await fetch("/api/custom/status", { headers: { Accept: "application/json" } });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
    state.providers = body.providers || [];
    const select = byId("custom-provider");
    const options = state.providers.map((provider) => {
      const suffix = provider.ready ? "" : ` (${provider.message})`;
      const option = element("option", "", `${provider.name}${suffix}`);
      option.value = provider.id;
      return option;
    });
    select.replaceChildren(...options);
    const previous = state.providers.find((provider) => provider.id === state.selectedProvider && provider.ready);
    const firstReady = state.providers.find((provider) => provider.ready);
    state.selectedProvider = previous?.id || firstReady?.id || state.providers[0]?.id || "";
    select.value = state.selectedProvider;
    renderProviderCapability();
  } catch {
    state.agentReady = false;
    state.providers = [];
    byId("custom-provider").replaceChildren();
    generate.disabled = true;
  }
}

function renderProviderCapability() {
  const provider = state.providers.find((candidate) => candidate.id === byId("custom-provider").value);
  state.selectedProvider = provider?.id || "";
  state.agentReady = Boolean(provider?.ready);
  byId("generate-custom").disabled = !state.agentReady;
}

function renderCustomJob(job) {
  const running = job.status === "queued" || job.status === "running";
  byId("custom-results").hidden = false;
  byId("custom-result-state").textContent = job.status === "completed"
    ? "PDF ready"
    : job.status === "failed"
      ? "Generation failed"
      : job.status === "running" ? `${job.providerName || "Agent"} is generating` : "Queued";
  byId("custom-results").className = `custom-results status-${job.status}`;
  byId("custom-result-time").textContent = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(job.updatedAt));
  byId("custom-result-summary").textContent = job.error || job.summary || (running ? "This can take a few minutes. You can leave this window open." : "");
  const links = (job.files || []).map((file) => {
    const link = element("a", "custom-file-link");
    link.href = file.url;
    link.download = file.name.split("/").at(-1);
    link.append(
      element("span", "custom-file-name", file.name),
      element("span", "custom-file-size", formatBytes(file.size)),
    );
    return link;
  });
  byId("custom-file-list").replaceChildren(...links);
  byId("generate-custom").disabled = running || !state.agentReady;
  byId("custom-prompt").disabled = running;
  byId("custom-provider").disabled = running;

  window.clearTimeout(state.customPoll);
  if (running) state.customPoll = window.setTimeout(() => loadCustomJob(job.id), 1800);
}

async function loadCustomJob(id) {
  try {
    const response = await fetch(`/api/custom/${encodeURIComponent(id)}`, { headers: { Accept: "application/json" } });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
    renderCustomJob(body);
  } catch (error) {
    renderCustomJob({ id, status: "failed", updatedAt: new Date().toISOString(), error: error.message, files: [] });
  }
}

async function createCustomFiles(event) {
  event.preventDefault();
  const prompt = byId("custom-prompt").value.trim();
  if (!prompt || !state.report || !state.agentReady || !state.selectedProvider) return;
  byId("generate-custom").disabled = true;
  byId("custom-prompt").disabled = true;
  byId("custom-provider").disabled = true;
  try {
    const response = await fetch("/api/custom", {
      method: "POST",
      headers: { Accept: "application/json", "Content-Type": "application/json" },
      body: JSON.stringify({
        provider: state.selectedProvider,
        prompt,
        space: state.selectedSpace,
        start: state.startDate,
        end: state.endDate,
      }),
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
    state.customJobID = body.id;
    renderCustomJob(body);
  } catch (error) {
    renderCustomJob({ id: "", status: "failed", updatedAt: new Date().toISOString(), error: error.message, files: [] });
  }
}

function refreshSpaceFilter(spaces, selectedSpace) {
  const select = byId("space-filter");
  const totalIssues = spaces.reduce((total, space) => total + space.issueCount, 0);
  const all = element("option", "", `All spaces · ${countLabel(totalIssues, "issue")}`);
  all.value = "";
  const options = [all];
  for (const space of spaces) {
    const label = space.name && space.name !== space.key ? `${space.name} (${space.key})` : space.key;
    const option = element("option", "", `${label} · ${countLabel(space.issueCount, "issue")}`);
    option.value = space.key;
    options.push(option);
  }
  select.replaceChildren(...options);
  select.value = selectedSpace;
}

function renderEngineers(engineers) {
  const resources = engineers;
  if (!resources.length) {
    replaceChildren("engineer-rows", [emptyRow(6, "No contributors were found in the selected issues.")]);
    return;
  }

  const maxHours = Math.max(...resources.map((engineer) => engineer.timeSpentHours));
  const rows = resources.flatMap((engineer, index) => {
    const row = element("tr");
    row.className = "contributor-summary-row";
    if (engineer.timeSpentHours === 0) row.classList.add("no-logged-time");
    const name = element("td", "contributor-name");
    const detailId = `contributor-work-${index}`;
    const toggle = element("button", "contributor-toggle");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", "false");
    toggle.setAttribute("aria-controls", detailId);
    toggle.append(
      element("span", "contributor-chevron", "›"),
      element("span", "contributor-label", engineer.name),
      element("span", "contributor-action", "Show work"),
    );
    name.append(toggle);
    const timeSpent = element("td", "effort-cell");
    timeSpent.append(element("span", "effort-value", formatHours(engineer.timeSpentHours)));
    const track = element("div", "effort-track");
    const progress = element("progress", "effort-progress");
    progress.max = maxHours || 1;
    progress.value = engineer.timeSpentHours;
    progress.setAttribute("aria-label", `${engineer.name} recorded hours`);
    track.append(progress);
    timeSpent.append(track);
    const share = element("td", "numeric", formatPercent(engineer.shareOfSprintPercent));
    const planned = element("td", "numeric", formatHours(engineer.plannedHours));
    const varianceClass = engineer.varianceHours > 0 ? "variance-positive" : engineer.varianceHours < 0 ? "variance-negative" : "";
    const variance = element("td", `numeric ${varianceClass}`, formatHours(engineer.varianceHours, true));
    const issuesWorked = element("td", "numeric", String(engineer.workedIssueCount));
    row.append(name, timeSpent, share, planned, variance, issuesWorked);

    const detailRow = element("tr", "contributor-detail-row");
    detailRow.id = detailId;
    detailRow.hidden = true;
    const detailCell = element("td", "contributor-detail-cell");
    detailCell.colSpan = 6;
    const activities = engineer.activities.filter((activity) => activity.actualHours > 0);
    const detailHead = element("div", "contributor-detail-head");
    const detailSummary = element("div", "contributor-detail-summary");
    const detailTitle = element("strong");
    const detailMetric = element("span", "contributor-type-metric");
    detailSummary.append(detailTitle, detailMetric);
    const typeFilterLabel = element("label", "contributor-type-filter");
    typeFilterLabel.append(element("span", "", "Work type"));
    const typeFilter = element("select");
    typeFilter.setAttribute("aria-label", `Filter ${engineer.name}'s issues by work type`);
    for (const type of ["All", "Story", "Bug", "Other"]) {
      const option = element("option", "", type === "All" ? "All work types" : type);
      option.value = type;
      typeFilter.append(option);
    }
    typeFilterLabel.append(typeFilter);
    detailHead.append(
      detailSummary,
      typeFilterLabel,
    );
    const detailWrap = element("div", "contributor-work-wrap");
    const detailTable = element("table", "contributor-work-table");
    const tableHead = element("thead");
    const headingRow = element("tr");
    for (const heading of ["Issue", "Summary", "Project", "Work type", "Time spent", "Planned"]) {
      const cell = element("th", "", heading);
      cell.scope = "col";
      headingRow.append(cell);
    }
    tableHead.append(headingRow);
    const tableBody = element("tbody");
    const renderContributorActivities = () => {
      const selectedType = typeFilter.value;
      const summary = contributorTypeSummary(activities, engineer.timeSpentHours, selectedType);
      detailTitle.textContent = `${engineer.name} · ${countLabel(summary.shown.length, "worked issue")}`;
      detailMetric.textContent = `${selectedType === "All" ? "All work" : selectedType} · ${formatPercent(summary.share)} of recorded time · ${formatHours(summary.hours)}`;

      const activityRows = summary.shown.map((activity) => {
        const activityRow = element("tr");
        const category = element("span", `category-tag ${categoryClass(activity.category)}`, activity.category);
        const categoryCell = element("td");
        categoryCell.append(category);
        activityRow.append(
          element("td", "issue-key", activity.issueKey),
          element("td", "contributor-work-summary", activity.summary),
          element("td", "numeric", activity.projectKey),
          categoryCell,
          element("td", "numeric", formatHours(activity.actualHours)),
          element("td", "numeric", formatHours(activity.plannedHours)),
        );
        return activityRow;
      });
      tableBody.replaceChildren(...(activityRows.length ? activityRows : [emptyRow(6, `No ${selectedType === "All" ? "worked" : selectedType} issues for this contributor.`)]));
    };
    typeFilter.addEventListener("change", renderContributorActivities);
    renderContributorActivities();
    detailTable.append(tableHead, tableBody);
    detailWrap.append(detailTable);
    detailCell.append(detailHead, detailWrap);
    detailRow.append(detailCell);

    toggle.addEventListener("click", () => {
      const expanded = toggle.getAttribute("aria-expanded") === "true";
      toggle.setAttribute("aria-expanded", String(!expanded));
      toggle.querySelector(".contributor-action").textContent = expanded ? "Show work" : "Hide work";
      detailRow.hidden = expanded;
    });
    return [row, detailRow];
  });
  replaceChildren("engineer-rows", rows);
}

function renderCategories(categories) {
  const bars = categories
    .filter((category) => category.actualHours > 0 || category.plannedHours > 0)
    .map((category) => {
      const item = element("div", "bar-item");
      const head = element("div", "bar-head");
      head.append(
        element("span", "bar-name", category.name),
        element("span", "bar-stat", `${formatHours(category.actualHours)} · ${formatPercent(category.shareOfActual)}`),
      );
      const progress = element("progress", `bar-progress ${categoryClass(category.name)}`);
      progress.max = 100;
      progress.value = Math.max(0, Math.min(category.shareOfActual, 100));
      progress.setAttribute("aria-label", `${category.name} share of actual effort`);
      item.append(head, progress);
      return item;
    });
  replaceChildren("category-bars", bars.length ? bars : [element("p", "empty-row", "No work allocation available.")]);
}

function renderProjects(projects) {
  const rows = projects.map((project) => {
    const row = element("div", "project-row");
    row.append(
      element("span", "project-name", project.name),
      element("span", "project-hours", formatHours(project.actualHours)),
      element("span", "project-share", formatPercent(project.shareOfActual)),
    );
    return row;
  });
  replaceChildren("project-list", rows.length ? rows : [element("p", "empty-row", "No project data available.")]);
}

function activityTypeSummary(types) {
  if (!types.length) return "No recorded Jira actions";
  return types.map((type) => `${type.count} ${type.label.toLowerCase()}`).join(" · ");
}

function renderActivityDefinitions(types) {
  const definitions = types.map((type) => {
    const item = element("div", "activity-definition");
    item.append(
      element("strong", "", type.label),
      element("span", "", type.description),
      element("span", "activity-definition-count", activityLabel(type.count)),
    );
    return item;
  });
  replaceChildren("activity-definition-list", definitions);
}

function renderActivityMap(people) {
  const buckets = activityBuckets(people);
  if (!buckets.length) {
    replaceChildren("activity-map", [element("p", "empty-row", "No contributors were found in the selected issues.")]);
    byId("activity-map-detail").replaceChildren();
    return;
  }

  if (!buckets.some((bucket) => bucket.count === state.selectedActivityCount)) {
    state.selectedActivityCount = buckets.some((bucket) => bucket.count === 0) ? 0 : buckets[0].count;
  }

  const renderSelection = () => {
    const selected = buckets.find((bucket) => bucket.count === state.selectedActivityCount);
    const heading = element("div", "activity-map-detail-heading");
    heading.append(
      element("strong", "", `${activityLabel(selected.count)} · ${peopleLabel(selected.people.length)}`),
      element("span", "", "Click another group to compare"),
    );
    const people = element("div", "activity-map-people");
    for (const personData of selected.people) {
      const person = element("div", "activity-map-person");
      person.append(
        element("strong", "", personData.name),
        element("span", "", activityTypeSummary(personData.types)),
      );
      people.append(person);
    }
    byId("activity-map-detail").replaceChildren(heading, people);
  };

  const buttons = buckets.map((bucket) => {
    const button = element("button", "activity-map-bucket");
    button.type = "button";
    button.style.setProperty("--bucket-size", Math.max(bucket.people.length, 1));
    button.setAttribute("aria-pressed", String(bucket.count === state.selectedActivityCount));
    button.setAttribute("aria-label", `${peopleLabel(bucket.people.length)} with ${activityLabel(bucket.count)}`);
    button.append(
      element("strong", "activity-map-count", String(bucket.count)),
      element("span", "activity-map-label", bucket.count === 1 ? "activity" : "activities"),
      element("span", "activity-map-total", peopleLabel(bucket.people.length)),
    );
    button.addEventListener("click", () => {
      state.selectedActivityCount = bucket.count;
      for (const item of byId("activity-map").children) {
        item.setAttribute("aria-pressed", String(item === button));
      }
      renderSelection();
    });
    return button;
  });
  replaceChildren("activity-map", buttons);
  renderSelection();
}

function refreshEngineerFilter(engineers) {
  const select = byId("engineer-filter");
  const previous = select.value;
  const resources = engineers.filter((engineer) => engineer.timeSpentHours > 0);
  const options = [element("option", "", "All contributors")];
  options[0].value = "";
  for (const engineer of resources) {
    const option = element("option", "", engineer.name);
    option.value = engineer.id;
    options.push(option);
  }
  select.replaceChildren(...options);
  if (resources.some((engineer) => engineer.id === previous)) select.value = previous;
}

function renderActivities() {
  const engineerID = byId("engineer-filter").value;
  const search = byId("issue-search").value.trim().toLowerCase();
  const shown = state.activities.filter((activity) => {
    const matchesEngineer = !engineerID || activity.engineerId === engineerID;
    const haystack = `${activity.issueKey} ${activity.summary} ${activity.projectKey} ${activity.category}`.toLowerCase();
    return matchesEngineer && (!search || haystack.includes(search));
  });

  const rows = shown.map((activity) => {
    const row = element("tr");
    const category = element("span", `category-tag ${categoryClass(activity.category)}`, activity.category);
    const categoryCell = element("td");
    categoryCell.append(category);
    row.append(
      element("td", "issue-key", activity.issueKey),
      element("td", "summary-cell", activity.summary),
      element("td", "", activity.engineer),
      element("td", "numeric", activity.projectKey),
      categoryCell,
      element("td", "numeric", formatHours(activity.actualHours)),
      element("td", "numeric", formatHours(activity.plannedHours)),
    );
    return row;
  });
  replaceChildren("activity-rows", rows.length ? rows : [emptyRow(7, "No issues match these filters.")]);
  byId("activity-count").textContent = `Showing ${shown.length} of ${state.activities.length} activities`;
}

function emptyRow(columns, message) {
  const row = element("tr");
  const cell = element("td", "empty-row", message);
  cell.colSpan = columns;
  row.append(cell);
  return row;
}

function render(report) {
  state.report = report;
  state.selectedSpace = report.selectedSpace || "";
  state.startDate = report.period.start;
  state.endDate = report.period.end;
  byId("period-start").value = state.startDate;
  byId("period-end").value = state.endDate;
  syncURL();
  state.activities = report.engineers.flatMap((engineer) => engineer.activities).filter((activity) => activity.actualHours > 0);
  refreshSpaceFilter(report.spaces, state.selectedSpace);
  renderSummary(report);
  renderEngineers(report.engineers);
  renderCategories(report.categories);
  renderProjects(report.projects);
  renderActivityDefinitions(report.activityTypes);
  renderActivityMap(report.activityPeople);
  refreshEngineerFilter(report.engineers);
  renderActivities();
  showMessage(report.warning || "");
}

async function loadReport(force = false) {
  if (state.refreshing) {
    state.pendingReload = true;
    return;
  }
  const requestedSpace = state.selectedSpace;
  const requestedStart = state.startDate;
  const requestedEnd = state.endDate;
  setLoading(true);
  try {
    const params = new URLSearchParams();
    if (requestedSpace) params.set("space", requestedSpace);
    if (requestedStart) params.set("start", requestedStart);
    if (requestedEnd) params.set("end", requestedEnd);
    if (force) params.set("refresh", "1");
    const query = params.toString();
    const response = await fetch(`/api/report${query ? `?${query}` : ""}`, {
      headers: { Accept: "application/json" },
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
    if (requestedSpace !== state.selectedSpace || requestedStart !== state.startDate || requestedEnd !== state.endDate) {
      state.pendingReload = true;
      return;
    }
    render(body);
    state.refreshedAt = Date.now();
  } catch (error) {
    showMessage(`Dashboard refresh failed: ${error.message}`, true);
    state.refreshedAt = Date.now();
  } finally {
    setLoading(false);
    if (state.pendingReload) {
      state.pendingReload = false;
      loadReport();
    }
  }
}

function updateCountdown() {
  if (!state.refreshedAt) return;
  const elapsed = Math.floor((Date.now() - state.refreshedAt) / 1000);
  const remaining = Math.max(0, refreshEvery - elapsed);
  byId("next-refresh").textContent = `Auto-refresh in ${remaining}s`;
  if (remaining === 0 && !state.refreshing && !document.hidden) loadReport();
}

byId("refresh-button").addEventListener("click", () => loadReport(true));
byId("custom-button").addEventListener("click", () => {
  const dialog = byId("custom-dialog");
  if (!dialog.open) dialog.showModal();
  loadCustomCapability();
  if (state.customJobID) loadCustomJob(state.customJobID);
});
byId("custom-close").addEventListener("click", () => byId("custom-dialog").close());
byId("custom-dialog").addEventListener("close", () => window.clearTimeout(state.customPoll));
byId("custom-form").addEventListener("submit", createCustomFiles);
byId("custom-provider").addEventListener("change", renderProviderCapability);
byId("apply-period").addEventListener("click", () => {
  const start = byId("period-start").value;
  const end = byId("period-end").value;
  if (!start || !end) {
    showMessage("Choose both a start date and an end date.", true);
    return;
  }
  if (start > end) {
    showMessage("The start date must not be after the end date.", true);
    return;
  }
  state.startDate = start;
  state.endDate = end;
  syncURL();
  loadReport();
});
byId("space-filter").addEventListener("change", () => {
  state.selectedSpace = byId("space-filter").value;
  syncURL();
  loadReport();
});
byId("engineer-filter").addEventListener("change", renderActivities);
byId("issue-search").addEventListener("input", renderActivities);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden && Date.now() - state.refreshedAt > refreshEvery * 1000) loadReport();
});

loadReport();
setInterval(updateCountdown, 1000);

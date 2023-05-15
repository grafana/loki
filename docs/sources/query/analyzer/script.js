Handlebars.registerHelper("inc", (val) => parseInt(val) + 1);
let streamSelector = `{job="analyze"}`;
const logsSourceInputElement = document.getElementById("logs-source-input");
const queryInputElement = document.getElementById("query-input");
const resultsElement = document.getElementById("results");

function initListeners() {
  [...document.getElementsByClassName("query_submit")].forEach(btn => btn.addEventListener("click", runQuery));
  [...document.getElementsByClassName("example-select")].forEach(btn => {
    btn.addEventListener("click", e => {
      if (!btn.checked) {
        return
      }
      loadExample(e.currentTarget.id);
      runQuery();
    });
  });
  document.getElementById("share-button").addEventListener("click", copySharableLink)
}

let linkCopiedNotificationElement = document.getElementById("share-link-copied-notification");

function copySharableLink() {
  linkCopiedNotificationElement.classList.add("hide")
  let extractedData = getDataFromInputs();
  let urlParam = new URLSearchParams();
  urlParam.set("query", extractedData.query);
  extractedData.logs.forEach(line => urlParam.append("line[]", line));
  let currentUrl = window.location.href;
  let sharableLink = window.location.origin + window.location.pathname + "?" + urlParam.toString();
  window.history.pushState(null, null, sharableLink);
  navigator.clipboard.writeText(sharableLink)
    .then(() => {
      linkCopiedNotificationElement.classList.remove("hide");
      setTimeout(() => linkCopiedNotificationElement.classList.add("hide"), 2000)
    })
}

function loadCheckedExample() {
  let selectedQueryExample = document.querySelector(".example-select:checked").id;
  loadExample(selectedQueryExample)
}

function updateInputs(logLines, query) {
  logsSourceInputElement.value = logLines.trim();
  queryInputElement.value = query.trim();
}

function loadExample(exampleId) {
  let logLinesElement = document.getElementById(exampleId + "-logs");
  let queryExampleElement = document.getElementById(exampleId + "-query");
  updateInputs(logLinesElement.innerText, queryExampleElement.innerText);
}

function toggleExplainSection(event) {
  let lineSection = document.getElementsByClassName("debug-result-row").item(event.currentTarget.dataset.lineIndex);
  [...lineSection.getElementsByClassName("debug-result-row__explain")].forEach(explainSection => explainSection.classList.toggle("hide"));
  [...lineSection.getElementsByClassName("line-cursor")].forEach(lineCursor => {
    lineCursor.classList.toggle("expand-cursor");
    lineCursor.classList.toggle("collapse-cursor");
  });
}

function getDataFromInputs() {
  let logs = logsSourceInputElement.value.split("\n");
  let query = queryInputElement.value;
  return {logs, query}
}

function runQuery() {
  resultsElement.classList.add("hide");
  const data = getDataFromInputs();
  sendRequest({...data, query: `${streamSelector} ${data.query}`})
    .then((response) => handleResponse(response))
}

async function handleResponse(response) {
  if (response.status !== 200) {
    handleError(await response.text());
    return
  }
  renderResponse(await response.json())
}

function handleError(error) {
  const template = Handlebars.compile("{{error_text}}");
  document.getElementById("query-error").innerHTML = template({error_text:error})
  document.getElementById("query-error").classList.remove("hide");
  resultsElement.classList.add("hide");
}

// Loki version in docs always looks like `MAJOR.MINOR.x`
const lokiVersionRegexp = /(\d+\.\d+\.x)/
const baseAnalyzerHost = "https://logql-analyzer.grafana.net";

function getSelectedDocsVersionTitle() {
  let selectedDocsVersion = document.querySelector("#grafana-version-select > option:checked");
  // we need to return "next" for the local development. in this case we do not have version selector
  return selectedDocsVersion && selectedDocsVersion.text || "next";
}

function getAnalyzerHost() {
  const docsVersionTitle = getSelectedDocsVersionTitle();
  const matches = docsVersionTitle.match(lokiVersionRegexp);
  if (matches === null || matches.length === 0) {
    return `${baseAnalyzerHost}/next`
  }
  return `${baseAnalyzerHost}/${matches[0].replaceAll(".", "-")}`
}

async function sendRequest(payload) {
  const host = getAnalyzerHost();
  return fetch(host + "/api/logql-analyze", {
    method: 'POST', headers: {
      'Accept': 'application/json', 'Content-Type': 'application/json'
    }, mode: 'cors', body: JSON.stringify(payload)
  });
}

function findAddedLabels(stageInfo) {
  const labelsBefore = new Map(stageInfo.labels_before.map(it => [it.name, it.value]));
  return stageInfo.labels_after
    .filter(labelValue => labelsBefore.get(labelValue.name) !== labelValue.value);
}

function adjustStagesModel(stages, response) {
  return stages.map((stageInfo, stageIndex) => {
    const addedLabels = findAddedLabels(stageInfo);
    return {
      ...stageInfo,
      labels_before: computeLabelColor(stageInfo.labels_before),
      labels_after: computeLabelColor(stageInfo.labels_after),
      added_labels: computeLabelColor(addedLabels),
      stage_expression: response.stages[stageIndex]
    }
  });
}

function adjustResponseModel(response) {
  return {
    ...response,
    results: response.results.map(result => {
      let stages = result.stage_records;
      return {
        ...result,
        log_result: stages.length > 0 ? stages[stages.length - 1].line_after : result.origin_line,
        filtered_out: stages.some(st => st.filtered_out),
        stages: adjustStagesModel(stages, response)
      }
    })
  }
}

function computeLabelColor(labels) {
  return labels.map(labelValue => {
    return {...labelValue, background_color: getBackgroundColor(labelValue.name)}
  })
}

function initResultsSectionListeners() {
  [...document.getElementsByClassName("last-stage-result")].forEach(row => row.addEventListener("click", toggleExplainSection));
}

function renderResponse(response) {
  resetErrorContainer();
  const adjustedResponse = adjustResponseModel(response);
  const rawResultsTemplate = document.getElementById("log-result-template").innerHTML;
  const template = Handlebars.compile(rawResultsTemplate);
  resultsElement.innerHTML = template(adjustedResponse);
  resultsElement.classList.remove("hide");
  initResultsSectionListeners();
}

function resetErrorContainer() {
  let errorContainer = document.getElementById("query-error");
  errorContainer.classList.add("hide")
  errorContainer.innerText = "";
}

function getBackgroundColor(stringInput) {
  let stringUniqueHash = [...stringInput].reduce((acc, char) => {
    return char.charCodeAt(0) + ((acc << 5) - acc);
  }, 0);
  return `hsl(${stringUniqueHash % 360}, 95%, 35%, 15%)`;
}

function loadExampleFromUrlIfExist() {
  const urlSearchParams = new URLSearchParams(window.location.search);
  const query = urlSearchParams.get("query");
  if (!query) {
    return
  }
  const logLines = urlSearchParams.getAll("line[]")
    .join("\n");
  updateInputs(logLines, query);
  runQuery()
}

initListeners();

loadCheckedExample();

loadExampleFromUrlIfExist();

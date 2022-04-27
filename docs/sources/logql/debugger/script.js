let host = "http://localhost:3001";
let streamSelector = `{job="debug"}`;
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
  logsSourceInputElement.innerHTML = logLines.trim();
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
  document.getElementById("query-error").innerHTML = error
  document.getElementById("query-error").classList.remove("hide");
  resultsElement.classList.add("hide");
}

async function sendRequest(payload) {
  return fetch(host + "/api/logql-debug", {
    method: 'POST', headers: {
      'Accept': 'application/json', 'Content-Type': 'application/json'
    }, mode: 'cors', body: JSON.stringify(payload)
  });
}

function findLastProcessed(stages) {
  return [...stages].reverse().find(stage => stage.processed)
}

function adjustResponseMode(response) {
  return {
    ...response, results: response.results.map(stages => {
      return {
        log_result: findLastProcessed(stages).line_after, filtered_out: stages.some(st => !st.successful), stages: stages.filter(stage => stage.processed)
          .map((stageInfo, stageIndex) => {
            const addedLabels = {};
            Object.keys(stageInfo.labels_after)
              .filter(label => {
                let valueBefore = stageInfo.labels_before[label];
                let valueAfter = stageInfo.labels_after[label];
                return valueBefore !== valueAfter;
              })
              .forEach(label => addedLabels[label] = stageInfo.labels_after[label]);
            return {
              ...stageInfo,
              labels_before: convertLabelsMapToArray(stageInfo.labels_before),
              labels_after: convertLabelsMapToArray(stageInfo.labels_after),
              added_labels: convertLabelsMapToArray(addedLabels),
              stage_expression: response.stages[stageIndex]
            }
          })
      }
    })
  }
}

function convertLabelsMapToArray(labels) {
  return Object.keys(labels)
    .sort()
    .map(labelName => {
      return {label_name: labelName, label_value: labels[labelName], background_color: getBackgroundColor(labelName)}
    })
}

function initResultsSectionListeners() {
  [...document.getElementsByClassName("last-stage-result")].forEach(row => row.addEventListener("click", toggleExplainSection));
}

function renderResponse(response) {
  resetErrorContainer();
  const adjustedResponse = adjustResponseMode(response);
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
  console.log(query)
  const logLines = urlSearchParams.getAll("line[]")
    .join("\n");
  updateInputs(logLines, query);
  runQuery()
}

initListeners();

loadCheckedExample();

loadExampleFromUrlIfExist();

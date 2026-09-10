import {
  fetchJson,
  fetchNothing,
  fetchString,
} from '@overleaf/fetch-utils'
import Settings from '@overleaf/settings'

const host = Settings.apis.clsi.url

function randomId() {
  // Avoid ids starting with 0, which get a dummy PDF served.
  return 'a' + Math.random().toString(16).slice(2)
}

function compile(projectId, data) {
  return fetchJson(`${host}/project/${projectId}/compile`, {
    method: 'POST',
    json: {
      compile: data,
    },
  })
}

async function stopCompile(projectId) {
  return await fetchNothing(`${host}/project/${projectId}/compile/stop`, {
    method: 'POST',
  })
}

async function clearCache(projectId) {
  return await fetchNothing(`${host}/project/${projectId}`, {
    method: 'DELETE',
  })
}

function getOutputFile(response, type) {
  for (const file of response.compile.outputFiles) {
    if (file.type === type && file.url.match(`output.${type}`)) {
      return file
    }
  }
  return null
}

function wordcount(projectId, file, compileRequest) {
  const url = new URL(`${host}/project/${projectId}/wordcount`)
  url.searchParams.append('file', file)
  if (compileRequest != null) {
    return fetchJson(url, {
      method: 'POST',
      json: {
        compile: compileRequest,
      },
    })
  }
  return fetchJson(url)
}

async function status(projectId) {
  return fetchString(`${host}/project/${projectId}/status`)
}

export default {
  randomId,
  compile,
  stopCompile,
  clearCache,
  getOutputFile,
  wordcount,
  status,
}

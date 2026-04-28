const BASE = ''

function normalizeConnection(connection) {
  if (!connection || typeof connection !== 'object') return connection

  const testStatus = connection.test_status ?? connection.status
  let status = testStatus
  if (testStatus === 'ok') status = 'success'
  if (testStatus === 'failed') status = 'error'

  return {
    ...connection,
    isActive: connection.isActive ?? connection.is_active,
    lastError: connection.lastError ?? connection.last_error,
    status,
  }
}

function normalizeProviderListResponse(data) {
  if (!data || typeof data !== 'object') return data
  return {
    ...data,
    connections: Array.isArray(data.connections) ? data.connections.map(normalizeConnection) : [],
    providers: Array.isArray(data.providers) ? data.providers.map(normalizeConnection) : data.providers,
  }
}

function normalizeProviderResponse(data) {
  if (!data || typeof data !== 'object') return data
  return {
    ...data,
    provider: normalizeConnection(data.provider),
    connection: normalizeConnection(data.connection),
  }
}

async function reqMaybeRaw(path, opts = {}) {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...getAuthHeader(), ...opts.headers },
    ...opts,
  })
  if (res.status === 401) {
    localStorage.removeItem('router_api_key')
    window.dispatchEvent(new Event('auth_error'))
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    let body = null
    try {
      body = await res.json()
    } catch {
      body = await res.text()
    }
    const message = body?.error?.message || body?.error || body?.message || body || res.statusText
    const err = new Error(`${res.status}: ${message}`)
    if (body && typeof body === 'object') {
      err.code = body.code || body?.error?.code
      err.details = body.details
      err.status = res.status
    }
    throw err
  }

  const contentType = res.headers.get('content-type') || ''
  if (contentType.includes('application/json')) {
    return { type: 'json', data: await res.json(), contentType }
  }
  return { type: 'text', data: await res.text(), contentType }
}

function getAuthHeader() {
  const token = localStorage.getItem('router_api_key')
  return token ? { 'Authorization': `Bearer ${token}` } : {}
}

async function req(path, opts = {}) {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...getAuthHeader(), ...opts.headers },
    ...opts,
  })
  if (res.status === 401) {
    localStorage.removeItem('router_api_key')
    window.dispatchEvent(new Event('auth_error'))
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    let body = null
    try {
      body = await res.json()
    } catch {
      body = await res.text()
    }
    const message = body?.error?.message || body?.error || body?.message || body || res.statusText
    const err = new Error(`${res.status}: ${message}`)
    if (body && typeof body === 'object') {
      err.code = body.code || body?.error?.code
      err.details = body.details
      err.status = res.status
    }
    throw err
  }
  return res.json()
}

function qs(params = {}) {
  const out = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') out.set(key, value)
  })
  const s = out.toString()
  return s ? `?${s}` : ''
}

export const api = {
  health: () => req('/healthz'),
  setupStatus: () => req('/api/setup/status'),
  metrics: () => req('/api/metrics/json'),
  config: () => req('/api/config'),
  updateConfig: (body) => req('/api/config', { method: 'PUT', body: JSON.stringify(body) }),
  providers: () => req('/api/providers').then(normalizeProviderListResponse),
  provider: (id) => req(`/api/providers/${encodeURIComponent(id)}`).then(normalizeProviderResponse),
  providersCatalog: (params = {}) => req(`/api/providers/catalog${qs(params)}`),
  validateProvider: (body) => req('/api/providers/validate', { method: 'POST', body: JSON.stringify(body) }),
  suggestedModels: (arg, url) => {
    const params = typeof arg === 'object' && arg !== null
      ? { ...arg, force_refresh: arg.forceRefresh ? 'true' : undefined }
      : { type: arg, url }
    return req(`/api/providers/suggested-models${qs(params)}`)
  },
  createProvider: (body) => req('/api/providers', { method: 'POST', body: JSON.stringify(body) }),
  updateProvider: (name, body) => req(`/api/providers/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteProvider: (name) => req(`/api/providers/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  testProvider: (name) => req(`/api/providers/${encodeURIComponent(name)}/test`, { method: 'POST' }),
  providerModels: (name, forceRefresh = false) => req(`/api/providers/${encodeURIComponent(name)}/models${forceRefresh ? '?force_refresh=true' : ''}`),
  providerNodes: () => req('/api/provider-nodes'),
  providerNode: (id) => req(`/api/provider-nodes/${encodeURIComponent(id)}`),
  createProviderNode: (body) => req('/api/provider-nodes', { method: 'POST', body: JSON.stringify(body) }),
  updateProviderNode: (id, body) => req(`/api/provider-nodes/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteProviderNode: (id) => req(`/api/provider-nodes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  proxyPools: () => req('/api/proxy-pools'),
  proxyPool: (id) => req(`/api/proxy-pools/${encodeURIComponent(id)}`),
  createProxyPool: (body) => req('/api/proxy-pools', { method: 'POST', body: JSON.stringify(body) }),
  updateProxyPool: (id, body) => req(`/api/proxy-pools/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteProxyPool: (id) => req(`/api/proxy-pools/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testProxyPool: (id) => req(`/api/proxy-pools/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  cliToolSettings: (tool) => req(`/api/cli-tools/${encodeURIComponent(tool)}-settings`),
  updateCliToolSettings: (tool, body) => req(`/api/cli-tools/${encodeURIComponent(tool)}-settings`, { method: 'POST', body: JSON.stringify(body) }),
  deleteCliToolSettings: (tool) => req(`/api/cli-tools/${encodeURIComponent(tool)}-settings`, { method: 'DELETE' }),
  mitmStatus: () => req('/api/cli-tools/antigravity-mitm'),
  mitmStart: (body) => req('/api/cli-tools/antigravity-mitm', { method: 'POST', body: JSON.stringify(body) }),
  mitmStop: () => req('/api/cli-tools/antigravity-mitm', { method: 'DELETE' }),
  mitmPatch: (body) => req('/api/cli-tools/antigravity-mitm', { method: 'PATCH', body: JSON.stringify(body) }),
  mitmAlias: (tool) => req(`/api/cli-tools/antigravity-mitm/alias${tool ? '?tool=' + encodeURIComponent(tool) : ''}`),
  updateMitmAlias: (body) => req('/api/cli-tools/antigravity-mitm/alias', { method: 'PUT', body: JSON.stringify(body) }),
  translatorLoad: () => req('/api/translator/load'),
  translatorSave: (body) => req('/api/translator/save', { method: 'POST', body: JSON.stringify(body) }),
  translatorTranslate: (body) => req('/api/translator/translate', { method: 'POST', body: JSON.stringify(body) }),
  translatorSend: (body) => req('/api/translator/send', { method: 'POST', body: JSON.stringify(body) }),
  translatorSendRaw: (body) => reqMaybeRaw('/api/translator/send', { method: 'POST', body: JSON.stringify(body) }),
  translatorConsoleLogs: () => req('/api/translator/console-logs'),
  clearTranslatorConsoleLogs: () => req('/api/translator/console-logs', { method: 'DELETE' }),
  oauthAuthorize: (provider, params = {}) => req(`/api/oauth/${encodeURIComponent(provider)}/authorize${qs(params)}`),
  oauthDeviceCode: (provider, params = {}) => req(`/api/oauth/${encodeURIComponent(provider)}/device-code${qs(params)}`),
  oauthExchange: (provider, body) => req(`/api/oauth/${encodeURIComponent(provider)}/exchange`, { method: 'POST', body: JSON.stringify(body) }),
  oauthPoll: (provider, body) => req(`/api/oauth/${encodeURIComponent(provider)}/poll`, { method: 'POST', body: JSON.stringify(body) }),
  providerHealth: (name, deep = false) => req(`/api/providers/${encodeURIComponent(name)}/health${deep ? '?deep=true' : ''}`),
  combos: () => req('/api/combos'),
  createCombo: (body) => req('/api/combos', { method: 'POST', body: JSON.stringify(body) }),
  updateCombo: (name, body) => req(`/api/combos/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteCombo: (name) => req(`/api/combos/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  aliases: () => req('/api/models/alias'),
  createAlias: (body) => req('/api/models/alias', { method: 'POST', body: JSON.stringify(body) }),
  updateAlias: (name, body) => req(`/api/models/alias/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteAlias: (name) => req(`/api/models/alias/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  models: () => req('/v1/models'),
  logs: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return req(`/api/logs${q ? '?' + q : ''}`)
  },
  logDetails: (id) => req(`/api/usage/request-details?request_id=${encodeURIComponent(id)}`),
  usage: () => req('/api/usage'),
  pricing: () => req('/api/pricing'),
  settings: () => req('/api/settings'),
  updateSettings: (body) => req('/api/settings', { method: 'PUT', body: JSON.stringify(body) }),
  keys: () => req('/api/keys'),
  createKey: (body) => req('/api/keys', { method: 'POST', body: JSON.stringify(body) }),
  updateKey: (id, body) => req(`/api/keys/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteKey: (id) => req(`/api/keys/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  oauthTokens: () => req('/api/oauth/tokens'),
  deleteOAuthToken: (provider, account) =>
    req(`/api/oauth/tokens/${provider}/${account}`, { method: 'DELETE' }),
  nodes: () => req('/api/nodes'),
}

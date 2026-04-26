const BASE = ''

async function req(path, opts = {}) {
  const res = await fetch(BASE + path, {
    headers: { 'Content-Type': 'application/json', ...opts.headers },
    ...opts,
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(`${res.status}: ${text}`)
  }
  return res.json()
}

export const api = {
  health: () => req('/healthz'),
  metrics: () => req('/api/metrics'),
  config: () => req('/api/config'),
  updateConfig: (body) => req('/api/config', { method: 'PUT', body: JSON.stringify(body) }),
  providers: () => req('/api/providers'),
  models: () => req('/v1/models'),
  logs: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return req(`/api/logs${q ? '?' + q : ''}`)
  },
  usage: () => req('/api/usage'),
  pricing: () => req('/api/pricing'),
  settings: () => req('/api/settings'),
  updateSettings: (body) => req('/api/settings', { method: 'PUT', body: JSON.stringify(body) }),
  oauthTokens: () => req('/api/oauth/tokens'),
  deleteOAuthToken: (provider, account) =>
    req(`/api/oauth/tokens/${provider}/${account}`, { method: 'DELETE' }),
  nodes: () => req('/api/nodes'),
}

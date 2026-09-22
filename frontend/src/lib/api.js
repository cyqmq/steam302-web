export async function api(method, url, body) {
  const init = { method, headers: {} }
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(body)
  }
  let res
  try {
    res = await fetch(url, init)
  } catch (e) {
    throw new Error('网络请求失败')
  }
  const text = await res.text()
  let data = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = text
  }
  if (!res.ok) {
    const msg =
      (data && (data.error || data.msg)) ||
      (typeof data === 'string' ? data : '') ||
      `HTTP ${res.status}`
    throw new Error(msg)
  }
  return data
}

export const get = (u) => api('GET', u)
export const post = (u, b) => api('POST', u, b)
export const put = (u, b) => api('PUT', u, b)
// Thin fetch wrapper. Every call is same-origin; a 401 sends the page back to
// the sign-in screen.
let onUnauthorized = () => {};
export const setUnauthorizedHandler = (fn) => { onUnauthorized = fn; };

export class ApiError extends Error {
  constructor(status, body) {
    super(body && body.error ? body.error : 'Request failed');
    this.status = status;
    this.hint = body && body.hint;
  }
}

async function request(method, path, { params, body, signal } = {}) {
  let url = path;
  if (params) {
    const q = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => { if (v !== undefined && v !== null && v !== '') q.set(k, v); });
    const s = q.toString();
    if (s) url += (path.includes('?') ? '&' : '?') + s;
  }
  const init = { method, credentials: 'same-origin', signal, headers: {} };
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(url, init);
  } catch (e) {
    if (e.name === 'AbortError') throw e;
    throw new ApiError(0, { error: 'Cannot reach the service.', hint: 'Check that parent-trackerd is running.' });
  }
  let data = null;
  try { data = await res.json(); } catch (e) { /* empty body */ }
  if (res.status === 401 && path !== '/api/login') onUnauthorized();
  if (!res.ok) throw new ApiError(res.status, data);
  return data;
}

// download fetches a file the server sends as an attachment and hands it to
// the browser as a save. A failure surfaces as an ApiError, like any request,
// instead of replacing the page with an error document.
async function download(path, params) {
  const q = new URLSearchParams();
  Object.entries(params || {}).forEach(([k, v]) => { if (v !== undefined && v !== null && v !== '') q.set(k, v); });
  let res;
  try {
    res = await fetch(path + '?' + q.toString(), { credentials: 'same-origin' });
  } catch (e) {
    throw new ApiError(0, { error: 'Cannot reach the service.', hint: 'Check that parent-trackerd is running.' });
  }
  if (!res.ok) {
    let data = null;
    try { data = await res.json(); } catch (e) { /* empty body */ }
    if (res.status === 401) onUnauthorized();
    throw new ApiError(res.status, data);
  }
  const m = /filename="([^"]+)"/.exec(res.headers.get('Content-Disposition') || '');
  const url = URL.createObjectURL(await res.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = m ? m[1] : 'download';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

export const api = {
  download,
  get: (path, params, signal) => request('GET', path, { params, signal }),
  post: (path, body) => request('POST', path, { body: body === undefined ? {} : body }),
  put: (path, body) => request('PUT', path, { body }),
};

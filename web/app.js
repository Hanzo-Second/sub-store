let lookupDestination = '';
let lookupRevision = 0;
let overrideSaving = false;
localStorage.removeItem('substore-servers');
let servers = [];
const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];
function escapeHTML(value = '') { return String(value).replace(/[&<>'"]/g, (char) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[char])); }
const serverFilters = { protocol: 'all', status: 'all' };
let workspaceSubscriptions = [];
const editIcon = '<svg viewBox="0 0 20 20"><path d="m13.8 4.2 2 2M5 15l.5-3.2L13.9 3.4a1.4 1.4 0 0 1 2 0l.7.7a1.4 1.4 0 0 1 0 2L8.2 14.5 5 15Z"/></svg>';
const trashIcon = '<svg viewBox="0 0 20 20"><path d="M4 6h12M8 6V4h4v2m-6 0 .7 10h6.6L14 6M8.5 9v4.5M11.5 9v4.5"/></svg>';
const copyIcon = '<svg viewBox="0 0 20 20"><rect x="7" y="7" width="9" height="9" rx="1.5"/><path d="M13 7V5.5A1.5 1.5 0 0 0 11.5 4h-6A1.5 1.5 0 0 0 4 5.5v6A1.5 1.5 0 0 0 5.5 13H7"/></svg>';
const saveIcon = '<svg viewBox="0 0 20 20"><path d="M4 3.5h10.5L17 6v10.5H4zM7 3.5V8h7V3.5M7 16.5v-5h7v5"/></svg>';
const usageIcon = '<svg viewBox="0 0 20 20"><path d="M4 16V9m4 7V5m4 11v-4m4 4V3"/></svg>';
const moveUpIcon = '<svg viewBox="0 0 20 20"><path d="m5 12 5-5 5 5"/></svg>';
const moveDownIcon = '<svg viewBox="0 0 20 20"><path d="m5 8 5 5 5-5"/></svg>';
const dragIcon = '<svg viewBox="0 0 20 20"><path d="M6 6h8M6 10h8M6 14h8"/></svg>';

function renderTrafficPlaceholder() {
  const panel = $('.traffic-panel');
  if (!panel) return;
  panel.innerHTML = '<div class="panel-header"><div><h2>Subscription usage</h2><p>Provider-reported data allowance</p></div><span class="telemetry-badge">PROVIDER DATA</span></div><div class="traffic-empty"><div class="empty-icon">↗</div><h3>No usage reported yet</h3><p>Update your Cheap / 光喵 subscription to see its data allowance here.</p></div>';
}
renderTrafficPlaceholder();

function addAccountSettings() {
  if ($('#account-form')) return;
  const form = document.createElement('form');
  form.id = 'account-form';
  form.className = 'panel account-settings-card';
  form.innerHTML = '<h2>Administrator account</h2><p>Change the username used to sign in, or set a new password. Your current password is required.</p><div class="settings-grid"><label>Username<input name="username" autocomplete="username" minlength="2" required /></label><label>Current password<input name="currentPassword" type="password" autocomplete="current-password" required /></label><label>New password<input name="newPassword" type="password" autocomplete="new-password" minlength="8" placeholder="Leave blank to keep current password" /></label><label>Confirm new password<input name="confirmNewPassword" type="password" autocomplete="new-password" minlength="8" placeholder="Repeat the new password" /></label></div><div class="account-settings-actions"><p class="account-settings-message" id="account-settings-message" aria-live="polite"></p><button class="button button-primary" type="submit">Save account</button></div>';
  $('#settings-form').insertAdjacentElement('afterend', form);
}
addAccountSettings();

function clearDemoContent() {
  $$('.kebab').forEach((button) => button.remove());
  $('.subscription-grid').innerHTML = '';
  $$('.rules-heading ~ .rule-row').forEach((row) => row.remove());
  $('.group-cards').innerHTML = '';
  $('#rule-provider-list').innerHTML = '';
  $('.activity-list').innerHTML = [['01', 'subscriptions', 'Connect your sources', 'Import and update provider subscriptions.'], ['02', 'groups', 'Choose how traffic flows', 'Organize sources into proxy groups.'], ['03', 'access-keys', 'Bring it to your devices', 'Create a private subscription URL.']].map(([number, view, title, detail]) => `<button class="workflow-link" data-view-target="${view}"><span>${number}</span><div><strong>${title}</strong><small>${detail}</small></div><span aria-hidden="true">↗</span></button>`).join('');
  $$('.stat-card .stat-value').forEach((value) => { value.textContent = '—'; });
  $$('.stat-card .stat-meta').forEach((value) => { value.textContent = 'No data yet'; });
  $('#view-config pre').textContent = '';
  $('#config-share-url-text').textContent = 'No access key selected';
}
clearDemoContent();

function serverRow(server, full = false) {
  const action = server.id < 0 ? '<span class="muted-text">Imported</span>' : `<span class="row-actions"><button class="icon-action" data-edit-server="${server.id}" aria-label="Edit ${escapeHTML(server.name)}" title="Edit server">${editIcon}</button><button class="icon-action danger" data-delete="${escapeHTML(server.name)}" data-delete-id="${server.id}" aria-label="Delete ${escapeHTML(server.name)}" title="Delete server">${trashIcon}</button></span>`;
  if (full) return `<div class="table-row"><div class="server-name"><div class="server-logo ${server.color || 'blue'}">${escapeHTML(server.logo || server.name[0])}</div><div><strong>${escapeHTML(server.name)}</strong><span>${server.port === '—' ? 'Imported provider' : 'Added manually'}</span></div></div><span class="protocol-tag">${server.type || 'Manual'}</span><span class="protocol-tag">${escapeHTML(server.protocol)}</span><span class="protocol-tag">${escapeHTML(server.address)}${server.port !== '—' ? `:${server.port}` : ''}</span><span class="status-pill ${server.status === 'Online' ? 'online' : 'offline'}"><i></i>${server.status}</span>${action}</div>`;
  return `<div class="table-row"><div class="server-name"><div class="server-logo ${server.color || 'blue'}">${escapeHTML(server.logo || server.name[0])}</div><div><strong>${escapeHTML(server.name)}</strong><span>${escapeHTML(server.address)}${server.port !== '—' ? `:${server.port}` : ''}</span></div></div><span class="protocol-tag">${escapeHTML(server.protocol)}</span><span class="latency">${server.latency || '—'}</span><span class="status-pill ${server.status === 'Online' ? 'online' : 'offline'}"><i></i>${server.status}</span>${action}</div>`;
}

function renderServers(filter = '') {
  const visible = servers.filter((server) => `${server.name} ${server.protocol} ${server.address}`.toLowerCase().includes(filter.toLowerCase()) && (serverFilters.protocol === 'all' || server.protocol.toLowerCase() === serverFilters.protocol) && (serverFilters.status === 'all' || server.status.toLowerCase() === serverFilters.status));
  $('#server-rows').innerHTML = servers.slice(0, 4).map((server) => serverRow(server)).join('') || '<div class="source-empty">No proxy servers yet. Add a manual proxy or import a subscription to get started.</div>';
  renderProxySections(visible);
  $('#active-server-stat').textContent = servers.filter((server) => server.status === 'Online').length;
  $('#proxy-nav-count').textContent = servers.length;
}

function renderProxySections(visible) {
  const container = $('#proxy-sections');
  if (!container) return;
  const manual = visible.filter((server) => server.id > 0);
  const imported = visible.filter((server) => server.id < 0);
  const sections = [];
  sections.push(`<section class="panel proxy-source-section"><div class="panel-header"><div><p class="source-kicker">MANUAL</p><h2>Manual proxy servers</h2><p>Standalone nodes added directly to this workspace.</p></div><span class="source-count">${manual.length}</span></div><div class="server-table"><div class="table-row table-head"><span>SERVER</span><span>TYPE</span><span>PROTOCOL</span><span>ADDRESS</span><span>STATUS</span><span></span></div>${manual.length ? manual.map((server) => serverRow(server, true)).join('') : '<div class="source-empty">No manual proxy servers yet.</div>'}</div></section>`);
  const bySubscription = new Map();
  imported.forEach((server) => { const source = server.name.split(' / ')[0]; if (!bySubscription.has(source)) bySubscription.set(source, []); bySubscription.get(source).push(server); });
  workspaceSubscriptions.forEach((subscription) => { if (!bySubscription.has(subscription.name)) bySubscription.set(subscription.name, []); });
  bySubscription.forEach((items, name) => { sections.push(`<section class="panel proxy-source-section subscription-source"><div class="panel-header"><div><p class="source-kicker">SUBSCRIPTION</p><h2>${escapeHTML(name)}</h2><p>Imported nodes from this subscription provider.</p></div><span class="source-count">${items.length}</span></div><div class="server-table"><div class="table-row table-head"><span>SERVER</span><span>TYPE</span><span>PROTOCOL</span><span>ADDRESS</span><span>STATUS</span><span></span></div>${items.length ? items.map((server) => serverRow(server, true)).join('') : '<div class="source-empty">No imported proxies yet. Update this subscription to fetch its nodes.</div>'}</div></section>`); });
  if (!sections.length) sections.push('<div class="panel empty-preview"><div class="empty-icon">⌁</div><h3>No proxy servers yet</h3><p>Add a manual server or subscription to build your proxy pool.</p></div>');
  container.innerHTML = sections.join('');
  $$('[data-edit-server]').forEach((button) => button.addEventListener('click', () => openModal(servers.find((server) => String(server.id) === button.dataset.editServer))));
  $$('[data-delete]').forEach((button) => button.addEventListener('click', () => deleteServer(button.dataset.delete, button.dataset.deleteId)));
}

function openModal(server = null) { const form = $('#proxy-form'); form.reset(); form.dataset.editId = server?.id || ''; $('#modal-title').textContent = server ? 'Edit proxy server' : 'Add proxy server'; form.querySelector('button[type="submit"]').textContent = server ? 'Save changes' : 'Add server'; if (server) { const protocol = String(server.protocol || '').toLowerCase().replace('shadowsocks', 'ss').replace('hysteria 2', 'hysteria2'); Object.entries({ name: server.name, protocol, address: server.address, port: server.port, credential: server.credential, transport: server.transport || 'TCP', sni: server.sni || '' }).forEach(([key, value]) => { if (form.elements[key]) form.elements[key].value = value; }); form.elements.skipVerify.checked = Boolean(server.skipVerify); } $('#modal-backdrop').classList.add('open'); $('#modal-backdrop').setAttribute('aria-hidden', 'false'); setTimeout(() => form.elements.name.focus(), 50); }
function closeModal() { $('#modal-backdrop').classList.remove('open'); $('#modal-backdrop').setAttribute('aria-hidden', 'true'); $('#proxy-form').reset(); $('#proxy-form').dataset.editId = ''; $('#modal-title').textContent = 'Add proxy server'; $('#proxy-form').querySelector('button[type="submit"]').textContent = 'Add server'; $('#advanced-fields').classList.remove('show'); $('#toggle-advanced').innerHTML = 'Show <span>⌄</span>'; }
function makeAccessKey() { const bytes = new Uint8Array(10); crypto.getRandomValues(bytes); return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join(''); }
async function copyText(value) {
  if (!value) return false;
  try { if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(value); return true; } } catch (_) { /* Fall back for non-secure local origins. */ }
  const input = document.createElement('textarea'); input.value = value; input.setAttribute('readonly', ''); input.style.cssText = 'position:fixed;opacity:0;pointer-events:none'; document.body.append(input); input.select();
  try { return document.execCommand('copy'); } catch (_) { return false; } finally { input.remove(); }
}
function subscriptionURL(key) { return `${(state.settings?.baseUrl || location.origin).replace(/\/$/, '')}/sub/${key}`; }
function showAccessKeyResult(key, title = 'Access key ready') { $('#access-result-title').textContent = title; $('#access-result-url').value = subscriptionURL(key); $('#access-result-backdrop').classList.add('open'); $('#access-result-backdrop').setAttribute('aria-hidden', 'false'); }
function closeAccessKeyResult() { $('#access-result-backdrop').classList.remove('open'); $('#access-result-backdrop').setAttribute('aria-hidden', 'true'); $('#access-result-url').value = ''; }
function setAccessKeyReplacement(form, replace) { form.elements.replaceKey.checked = replace; $('.key-mode-tabs').style.display = replace ? 'grid' : 'none'; $('#generated-key-field').classList.toggle('hide', !replace); $('#manual-key-field').classList.toggle('show', replace && $('.key-mode.active').dataset.keyMode === 'manual'); $('[name="manualKey"]').required = replace && $('.key-mode.active').dataset.keyMode === 'manual'; $('[name="generatedKey"]').required = replace && $('.key-mode.active').dataset.keyMode !== 'manual'; if (replace && !form.elements.generatedKey.value) form.elements.generatedKey.value = makeAccessKey(); }
function ensureAccessEditField() { const form = $('#access-form'); if (form.elements.enabled) return; const enabled = document.createElement('label'); enabled.className = 'switch-label access-enabled-field'; enabled.innerHTML = '<span>Enable access key</span><input name="enabled" type="checkbox" checked /><i class="switch"></i>'; const replace = document.createElement('label'); replace.className = 'switch-label access-replace-field'; replace.innerHTML = '<span>Replace subscription key</span><input name="replaceKey" type="checkbox" /><i class="switch"></i>'; form.insertBefore(enabled, form.querySelector('.modal-footer')); form.insertBefore(replace, form.querySelector('.key-mode-tabs')); form.elements.replaceKey.addEventListener('change', () => setAccessKeyReplacement(form, form.elements.replaceKey.checked)); }
function updateAccessUsageMode() { const form = $('#access-form'); form.elements.monthlyDataGB.disabled = form.elements.usageSubscriptionId.value !== '0'; }
function populateAccessUsageSubscriptions(selectedId = 0) { const select = $('#access-form [name="usageSubscriptionId"]'); select.innerHTML = '<option value="0">Manual monthly allowance</option>' + state.subscriptions.map((item) => `<option value="${item.id}">${escapeHTML(item.name)}${item.enabled ? '' : ' (disabled)'}</option>`).join(''); select.value = String(selectedId || 0); updateAccessUsageMode(); }
function openAccessModal(item = null) { ensureAccessEditField(); const form = $('#access-form'); form.reset(); form.dataset.editId = item?.id || ''; populateAccessUsageSubscriptions(item?.usageSubscriptionId); $('#access-modal-title').textContent = item ? 'Edit access key' : 'Add access key'; form.querySelector('button[type="submit"]').textContent = item ? 'Save changes' : 'Create access key'; $('.access-replace-field').style.display = item ? 'flex' : 'none'; $('.key-mode-tabs').style.display = item ? 'none' : 'grid'; $('#generated-key-field').classList.toggle('hide', Boolean(item)); $('#manual-key-field').classList.remove('show'); $('[name="manualKey"]').required = false; $('[name="generatedKey"]').required = !item; if (item) { form.elements.keyName.value = item.name; form.elements.monthlyDataGB.value = item.monthlyDataGB || 0; form.elements.enabled.checked = item.enabled; setAccessKeyReplacement(form, false); } else { $$('[data-key-mode]').forEach((button) => { const active = button.dataset.keyMode === 'generated'; button.classList.toggle('active', active); button.setAttribute('aria-pressed', String(active)); }); $('[name="generatedKey"]').value = makeAccessKey(); form.elements.monthlyDataGB.value = 0; form.elements.enabled.checked = true; } $('#access-modal-backdrop').classList.add('open'); $('#access-modal-backdrop').setAttribute('aria-hidden', 'false'); setTimeout(() => form.elements.keyName.focus(), 50); }
function closeAccessModal() { $('#access-modal-backdrop').classList.remove('open'); $('#access-modal-backdrop').setAttribute('aria-hidden', 'true'); $('#access-form').reset(); $('#access-form').dataset.editId = ''; $('#access-modal-title').textContent = 'Add access key'; $('#access-form').querySelector('button[type="submit"]').textContent = 'Create access key'; $('.access-replace-field').style.display = 'none'; $('.key-mode-tabs').style.display = 'grid'; $('#generated-key-field').classList.remove('hide'); }
$('#access-form [name="usageSubscriptionId"]').addEventListener('change', updateAccessUsageMode);
function showToast() { $('#toast').classList.add('show'); setTimeout(() => $('#toast').classList.remove('show'), 3400); }
async function deleteServer(name, id) { if (id) { if (!(await confirmDelete(`Delete “${name}”?`, 'This manual proxy will be permanently removed from SubStore.'))) return; try { await api(`/api/proxies/${id}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } return; } servers = servers.filter((server) => server.name !== name); localStorage.setItem('substore-servers', JSON.stringify(servers)); renderServers($('#server-search')?.value || ''); }

function selectView(viewName) {
  if (!$(`#view-${viewName}`)) return;
  $$('.nav-item[data-view]').forEach((item) => { item.classList.toggle('active', item.dataset.view === viewName); if (item.dataset.view === viewName) item.setAttribute('aria-current', 'page'); else item.removeAttribute('aria-current'); });
  $('#sidebar').classList.remove('open');
  $('#mobile-menu').setAttribute('aria-expanded', 'false');
  $('#mobile-menu').setAttribute('aria-label', 'Open navigation');
  $$('.view').forEach((view) => view.classList.remove('active-view'));
  $(`#view-${viewName}`).classList.add('active-view');
  localStorage.setItem('substore-active-view', viewName);
  const labels = { 'rule-providers': 'Rule providers', 'access-keys': 'Access keys', overview: 'Dashboard', 'client-config': 'Client configuration', config: 'Generated config', proxies: 'Proxy servers', groups: 'Proxy groups', rules: 'Routing rules' };
  const label = labels[viewName] || viewName.charAt(0).toUpperCase() + viewName.slice(1);
  $('#page-breadcrumb').textContent = label;
  $('.page-body').scrollTop = 0;
  updateBackToTopButton();
}

function renderConfigYAML(yaml) {
  const pre = $('#view-config pre');
  if (!pre) return;
  const lines = String(yaml || '').split('\n');
  const sections = [];
  pre.replaceChildren();
  lines.forEach((line, index) => {
    const span = document.createElement('span');
    span.textContent = line + (index < lines.length - 1 ? '\n' : '');
    const topLevel = line.match(/^([A-Za-z0-9_-]+):/);
    if (topLevel) {
      const section = topLevel[1];
      span.id = `config-section-${section}`;
      span.className = 'config-section-marker';
      sections.push(section);
    }
    pre.appendChild(span);
  });
  renderConfigSectionMenu(sections);
}

function configSectionLabel(section) {
  const labels = { 'mixed-port': 'Mixed port', 'allow-lan': 'Allow LAN', 'bind-address': 'Bind address', mode: 'Mode', 'log-level': 'Log level', dns: 'DNS', proxies: 'Proxies', 'proxy-groups': 'Proxy groups', 'rule-providers': 'Rule providers', rules: 'Rules' };
  return labels[section] || section.split('-').map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join(' ');
}

function renderConfigSectionMenu(sections) {
  const links = $('.config-quick-links');
  if (!links) return;
  links.innerHTML = sections.map((section) => `<button type="button" class="config-quick-link" data-config-section="${escapeHTML(section)}"><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M5 5h10M5 10h10M5 15h10"/><circle cx="3" cy="5" r=".7"/><circle cx="3" cy="10" r=".7"/><circle cx="3" cy="15" r=".7"/></svg>${escapeHTML(configSectionLabel(section))}<span>→</span></button>`).join('');
}

function scrollToConfigSection(section) {
  const scroller = $('.page-body');
  const target = $(`#config-section-${section}`);
  if (!scroller || !target) return;
  const top = target.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop - 22;
  scroller.scrollTo({ top, behavior: 'smooth' });
}

function updateBackToTopButton() {
  const scroller = $('.page-body');
  const button = $('#back-to-top');
  if (!scroller || !button) return;
  const visible = scroller.scrollTop > 320;
  button.classList.toggle('show', visible);
  button.setAttribute('aria-hidden', String(!visible));
  button.tabIndex = visible ? 0 : -1;
}

$('.page-body').addEventListener('scroll', updateBackToTopButton, { passive: true });
$('#back-to-top').addEventListener('click', () => $('.page-body').scrollTo({ top: 0, behavior: 'smooth' }));

$('.config-quick-nav > p').textContent = 'Jump to each section inside the generated YAML.';
$('.config-quick-links').innerHTML = '<span class="config-quick-loading">Loading config sections…</span>';
$('.config-quick-links').addEventListener('click', (event) => { const button = event.target.closest('[data-config-section]'); if (button) scrollToConfigSection(button.dataset.configSection); });
$$('.nav-item[data-view]').forEach((item) => item.addEventListener('click', () => selectView(item.dataset.view)));
$$('[data-view-target]').forEach((item) => item.addEventListener('click', () => selectView(item.dataset.viewTarget)));
$('#add-server').addEventListener('click', openModal);
$('#close-modal').addEventListener('click', closeModal);
$('#cancel-modal').addEventListener('click', closeModal);
$('#modal-backdrop').addEventListener('click', (event) => { if (event.target === $('#modal-backdrop')) closeModal(); });
document.addEventListener('keydown', (event) => { if (event.key === 'Escape') { closeModal(); closeAccessModal(); closeAccessKeyResult(); } });
$('#toggle-advanced').addEventListener('click', () => { const open = $('#advanced-fields').classList.toggle('show'); $('#toggle-advanced').innerHTML = open ? 'Hide <span>⌃</span>' : 'Show <span>⌄</span>'; });
$('#protocol-select').addEventListener('change', (event) => { const hint = $('#credential-hint'); const hints = { vless: 'VLESS UUID', vmess: 'VMess UUID', trojan: 'Trojan password', ss: 'Base64 or plain password', hysteria2: 'Hysteria 2 password', tuic: 'TUIC UUID / password', socks5: 'Username:password (optional)', http: 'Username:password (optional)' }; hint.textContent = hints[event.target.value]; });
$('#server-search').addEventListener('input', (event) => renderServers(event.target.value));
$('#add-access-key').addEventListener('click', openAccessModal);
['#copy-generated-key', '#copy-access-url'].forEach((selector) => { const button = $(selector); if (button) button.innerHTML = copyIcon; });
$('#manage-access-keys').addEventListener('click', () => selectView('access-keys'));
$('#close-access-modal').addEventListener('click', closeAccessModal);
$('#cancel-access-modal').addEventListener('click', closeAccessModal);
$('#access-modal-backdrop').addEventListener('click', (event) => { if (event.target === $('#access-modal-backdrop')) closeAccessModal(); });
$('#copy-generated-key').addEventListener('click', async () => { const copied = await copyText($('[name="generatedKey"]').value); showNotice(copied ? 'Access key copied' : 'Copy failed', copied ? 'Save it securely before creating the key.' : 'Click the key and copy it manually.'); });
$$('[name="generatedKey"], #access-result-url').forEach((input) => input.addEventListener('click', () => input.select()));
$('#close-access-result').addEventListener('click', closeAccessKeyResult);
$('#done-access-result').addEventListener('click', closeAccessKeyResult);
$('#access-result-backdrop').addEventListener('click', (event) => { if (event.target === $('#access-result-backdrop')) closeAccessKeyResult(); });
$('#copy-access-url').addEventListener('click', async () => { const copied = await copyText($('#access-result-url').value); showNotice(copied ? 'Subscription URL copied' : 'Copy failed', copied ? 'Paste it into Clash, Mihomo, or OpenClash.' : 'Select the URL and copy it manually.'); });
$('#regenerate-key').addEventListener('click', () => { $('[name="generatedKey"]').value = makeAccessKey(); });
$$('[data-key-mode]').forEach((button) => button.addEventListener('click', () => { $$('[data-key-mode]').forEach((item) => { item.classList.toggle('active', item === button); item.setAttribute('aria-pressed', String(item === button)); }); const manual = button.dataset.keyMode === 'manual'; $('#manual-key-field').classList.toggle('show', manual); $('#generated-key-field').classList.toggle('hide', manual); $('[name="manualKey"]').required = manual; $('[name="generatedKey"]').required = !manual; }));
renderServers();
const savedView = localStorage.getItem('substore-active-view');
selectView(savedView && document.getElementById(`view-${savedView}`) ? savedView : 'overview');

const apiBase = location.protocol === 'file:' ? 'http://localhost:8080' : '';
const api = async (path, options = {}) => {
  const response = await fetch(`${apiBase}${path}`, { credentials: 'include', headers: { 'Content-Type': 'application/json', ...(options.headers || {}) }, ...options });
  const contentType = response.headers.get('content-type') || '';
  const body = contentType.includes('json') ? await response.json() : await response.text();
  if (!response.ok) {
    const error = new Error(body?.error || body || 'Request failed');
    error.status = response.status;
    error.details = typeof body === 'object' && body ? body : null;
    throw error;
  }
  return body;
};

function closeDependencyDialog() {
  const backdrop = $('#dependency-dialog-backdrop');
  if (!backdrop) return;
  backdrop.classList.remove('open');
  backdrop.setAttribute('aria-hidden', 'true');
}

function showRequestError(error) {
  const details = error?.details;
  if (!Array.isArray(details?.dependencies) || !details.dependencies.length) {
    alert(error?.message || 'Request failed');
    return;
  }
  let backdrop = $('#dependency-dialog-backdrop');
  if (!backdrop) {
    backdrop = document.createElement('div');
    backdrop.id = 'dependency-dialog-backdrop';
    backdrop.className = 'modal-backdrop';
    backdrop.setAttribute('aria-hidden', 'true');
    backdrop.innerHTML = '<div class="modal dependency-dialog" role="dialog" aria-modal="true" aria-labelledby="dependency-dialog-title"><div class="modal-header"><div><p class="eyebrow">ITEM IN USE</p><h2 id="dependency-dialog-title">Cannot delete</h2><p id="dependency-dialog-summary"></p></div><button type="button" class="close-button" data-close-dependency aria-label="Close">×</button></div><div class="dependency-dialog-body"><strong>Used by</strong><ul id="dependency-dialog-list"></ul><p>Remove this item from the records above, then try deleting it again.</p></div><div class="dependency-dialog-footer"><button type="button" class="button button-primary" data-close-dependency>OK</button></div></div>';
    document.body.append(backdrop);
    backdrop.addEventListener('click', (event) => {
      if (event.target === backdrop || event.target.closest('[data-close-dependency]')) closeDependencyDialog();
    });
  }
  $('#dependency-dialog-title').textContent = `Cannot delete ${details.kind || 'item'}`;
  $('#dependency-dialog-summary').textContent = `“${details.name || 'This item'}” is still referenced by other configuration items.`;
  $('#dependency-dialog-list').innerHTML = details.dependencies.map((item) => `<li>${escapeHTML(item)}</li>`).join('');
  backdrop.classList.add('open');
  backdrop.setAttribute('aria-hidden', 'false');
  setTimeout(() => backdrop.querySelector('[data-close-dependency]').focus(), 50);
}

let deleteConfirmationResolve = null;
function closeDeleteConfirmation(confirmed = false) {
  const backdrop = $('#delete-confirm-backdrop');
  if (!backdrop) return;
  backdrop.classList.remove('open');
  backdrop.setAttribute('aria-hidden', 'true');
  const resolve = deleteConfirmationResolve;
  deleteConfirmationResolve = null;
  resolve?.(confirmed);
}

function confirmDelete(title, message) {
  let backdrop = $('#delete-confirm-backdrop');
  if (!backdrop) {
    backdrop = document.createElement('div');
    backdrop.id = 'delete-confirm-backdrop';
    backdrop.className = 'modal-backdrop';
    backdrop.setAttribute('aria-hidden', 'true');
    backdrop.innerHTML = `<div class="modal delete-confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="delete-confirm-title"><div class="modal-header"><div><p class="eyebrow">CONFIRM DELETION</p><h2 id="delete-confirm-title">Delete item?</h2><p id="delete-confirm-message"></p></div><button type="button" class="close-button" data-delete-cancel aria-label="Cancel deletion">×</button></div><div class="delete-confirm-warning"><span>${trashIcon}</span><p>This action cannot be undone.</p></div><div class="delete-confirm-footer"><button type="button" class="button button-secondary" data-delete-cancel>Cancel</button><button type="button" class="button button-danger" data-delete-confirm>${trashIcon}<span>Delete</span></button></div></div>`;
    document.body.append(backdrop);
    backdrop.addEventListener('click', (event) => {
      if (event.target === backdrop || event.target.closest('[data-delete-cancel]')) closeDeleteConfirmation(false);
      else if (event.target.closest('[data-delete-confirm]')) closeDeleteConfirmation(true);
    });
  }
  if (deleteConfirmationResolve) closeDeleteConfirmation(false);
  $('#delete-confirm-title').textContent = title;
  $('#delete-confirm-message').textContent = message;
  backdrop.classList.add('open');
  backdrop.setAttribute('aria-hidden', 'false');
  setTimeout(() => backdrop.querySelector('[data-delete-confirm]').focus(), 50);
  return new Promise((resolve) => { deleteConfirmationResolve = resolve; });
}

const state = { setupRequired: false, settings: null, accessKeys: [], subscriptions: [], groups: [], providers: [], serviceRuleGroups: [], user: null };

function showAuth(setup = false) {
  $('#auth-screen').hidden = false;
  $('.app-shell').inert = true;
  $('#auth-eyebrow').textContent = setup ? 'FIRST RUN SETUP' : 'PRIVATE CONTROL PLANE';
  $('#auth-title').textContent = setup ? 'Create your admin account' : 'Welcome back';
  $('#auth-subtitle').textContent = setup ? 'Set up the account used to protect this self-hosted instance.' : 'Sign in to manage your proxy network.';
  $('#auth-hint').textContent = setup ? 'Use at least 8 characters.' : '';
  $('#auth-submit').textContent = setup ? 'Create account' : 'Sign in';
  $('#auth-form').dataset.setup = setup ? 'true' : 'false';
  $('#auth-error').textContent = '';
}

function hideAuth() { $('#auth-screen').hidden = true; $('.app-shell').inert = false; }
function renderUser() { if (!state.user) return; const name = state.user.username || 'Admin'; $('.user-avatar').textContent = name[0].toUpperCase(); $('.user-card strong').textContent = name; $('.user-card span').textContent = state.user.role || 'Administrator'; const username = $('#account-form [name="username"]'); if (username) username.value = name; }

function renderAccessKeys() {
  const rows = $('#access-key-rows');
  if (!rows) return;
  rows.innerHTML = state.accessKeys.length ? state.accessKeys.map((key) => { const url = key.key ? subscriptionURL(key.key) : `${(state.settings?.baseUrl || location.origin).replace(/\/$/, '')}/sub/${key.keyPreview}`; const copy = key.key ? `<button class="icon-action" data-copy-access="${key.id}" aria-label="Copy URL for ${escapeHTML(key.name)}" title="Copy subscription URL">${copyIcon}</button>` : ''; const usage = key.usageSubscriptionName ? `Mirrors ${key.usageSubscriptionName}` : key.monthlyDataGB > 0 ? `${Number(key.monthlyDataGB).toLocaleString()} GB/month` : 'Not published'; return `<div class="access-key-row"><div class="key-value"><div class="key-icon">⌘</div><div><strong>${escapeHTML(key.name)}</strong><span>${escapeHTML(url)}</span></div></div><span>Private subscription</span><span class="status-pill ${key.enabled ? 'online' : 'offline'}"><i></i>${key.enabled ? 'Active' : 'Disabled'}</span><span class="muted-text">${escapeHTML(usage)}</span><span class="muted-text">${key.lastUsedAt ? escapeHTML(key.lastUsedAt) : 'Never'}</span><span class="row-actions">${copy}<button class="icon-action" data-edit-access="${key.id}" aria-label="Edit ${escapeHTML(key.name)}" title="Edit access key">${editIcon}</button><button class="icon-action danger" data-delete-access="${key.id}" aria-label="Delete ${escapeHTML(key.name)}" title="Delete access key">${trashIcon}</button></span></div>`; }).join('') : '<div class="empty-preview"><div class="empty-icon">⌘</div><h3>No access keys yet</h3><p>Create one to share your generated configuration.</p></div>';
  $$('[data-copy-access]').forEach((button) => button.addEventListener('click', async () => { const item = state.accessKeys.find((key) => String(key.id) === button.dataset.copyAccess); const copied = await copyText(item ? subscriptionURL(item.key) : ''); showNotice(copied ? 'Subscription URL copied' : 'Copy failed', copied ? 'Paste it into Clash, Mihomo, or OpenClash.' : 'Try again or replace the key.'); }));
  $$('[data-edit-access]').forEach((button) => button.addEventListener('click', () => openAccessModal(state.accessKeys.find((key) => String(key.id) === button.dataset.editAccess))));
  $$('[data-delete-access]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this access key?', 'Clients using this private subscription URL will lose access.'))) return; try { await api(`/api/access-keys/${button.dataset.deleteAccess}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
  renderConfigAccessKeyPicker();
}

function renderConfigAccessKeyPicker() {
  const select = $('#config-access-key-select');
  if (!select) return;
  const saved = localStorage.getItem('substore-config-access-key-id');
  select.innerHTML = state.accessKeys.length ? state.accessKeys.map((key) => `<option value="${key.id}" ${key.enabled ? '' : 'disabled'}>${escapeHTML(key.name)}${key.enabled ? '' : ' (disabled)'}${key.key ? '' : ' — replace key required'}</option>`).join('') : '<option value="">No access keys</option>';
  const preferred = state.accessKeys.find((key) => String(key.id) === saved && key.enabled) || state.accessKeys.find((key) => key.enabled) || state.accessKeys[0];
  select.value = preferred ? String(preferred.id) : '';
  updateConfigAccessKeyURL();
}
function updateConfigAccessKeyURL() {
  const id = $('#config-access-key-select').value;
  const key = state.accessKeys.find((item) => String(item.id) === id);
  const url = key?.key ? subscriptionURL(key.key) : '';
  $('#config-share-url-text').textContent = url || (key ? 'Replace this key to access its URL.' : 'No access key selected');
  $('#config-access-key-hint').textContent = key?.key ? '' : key ? 'Replace this access key once to make it available here.' : 'Create an access key to get a private subscription URL.';
  $('#copy-config-access-url').disabled = !url;
  updateCombinedUsageHint();
}

function updateCombinedUsageHint() {
  const usage = $('#combined-usage');
  if (!usage) return;
  const key = state.accessKeys.find((item) => String(item.id) === $('#config-access-key-select')?.value);
  const source = state.subscriptions.find((item) => String(item.id) === String(key?.usageSubscriptionId));
  if (source) {
    usage.textContent = source.totalGB > 0 ? `Usage mirrors ${source.name}: ${Number(source.usedGB || 0).toFixed(2)} / ${Number(source.totalGB).toFixed(2)} GB` : `Usage mirrors ${source.name}, but that provider has not reported an allowance yet.`;
  } else if (key?.monthlyDataGB > 0) {
    usage.textContent = `Usage publishes a manual ${Number(key.monthlyDataGB).toLocaleString()} GB monthly allowance.`;
  } else {
    usage.textContent = key ? 'No usage is published. Edit this access key to choose a subscription.' : 'Select an access key to view its usage source.';
  }
}

async function refreshWorkspace() {
  const [proxyData, subscriptions, rules, accessKeys, settings] = await Promise.all([api('/api/proxies'), api('/api/subscriptions'), api('/api/rules'), api('/api/access-keys'), api('/api/settings')]);
  servers = proxyData.map((server) => ({ ...server, status: server.enabled ? 'Online' : 'Offline', type: server.id < 0 ? 'Subscription' : 'Manual', latency: server.latency || '—', logo: server.name[0]?.toUpperCase() || 'P', color: server.id < 0 ? 'purple' : 'blue' }));
  state.accessKeys = accessKeys;
  state.subscriptions = subscriptions;
  workspaceSubscriptions = subscriptions;
  state.settings = settings;
  renderServers($('#server-search')?.value || '');
  renderAccessKeys();
  $('.nav-count').textContent = subscriptions.length;
  const ruleCount = $('.health-row strong');
  if (ruleCount && rules) ruleCount.textContent = `${rules.length} custom`;
  const settingsForm = $('#settings-form');
  if (settingsForm) Object.entries(settings).forEach(([key, value]) => { const field = settingsForm.elements[key]; if (!field) return; if (field.type === 'checkbox') field.checked = value; else field.value = value; });
}

async function bootApp() {
  try {
    const bootstrap = await api('/api/bootstrap');
    state.setupRequired = bootstrap.setupRequired;
    if (bootstrap.setupRequired) { showAuth(true); return; }
    try { state.user = await api('/api/auth/me'); renderUser(); } catch { showAuth(false); return; }
    try { await refreshWorkspace(); } catch (error) { showRequestError(error); }
  } catch (error) {
    showAuth(false);
    $('#auth-error').textContent = apiBase ? 'SubStore is not running. Start it with “go run .” or “docker compose up --build”, then reload this page.' : `Could not connect to SubStore: ${error.message}`;
  }
}

$('#auth-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const data = Object.fromEntries(new FormData(event.target));
  const endpoint = event.target.dataset.setup === 'true' ? '/api/auth/setup' : '/api/auth/login';
  try { state.user = await api(endpoint, { method: 'POST', body: JSON.stringify(data) }); renderUser(); hideAuth(); await refreshWorkspace(); } catch (error) { $('#auth-error').textContent = error.message; }
});

document.addEventListener('submit', async (event) => {
  if (event.target.id !== 'proxy-form') return;
  event.preventDefault();
  event.stopImmediatePropagation();
  const data = Object.fromEntries(new FormData(event.target));
  const id = event.target.dataset.editId;
  try { await api(id ? `/api/proxies/${id}` : '/api/proxies', { method: id ? 'PATCH' : 'POST', body: JSON.stringify({ ...data, port: Number(data.port), skipVerify: data.skipVerify === 'on', enabled: true }) }); closeModal(); await refreshWorkspace(); selectView('proxies'); showToast(); } catch (error) { alert(error.message); }
}, true);

document.addEventListener('submit', async (event) => {
  if (event.target.id !== 'access-form') return;
  event.preventDefault();
  event.stopImmediatePropagation();
  const data = Object.fromEntries(new FormData(event.target));
  const id = event.target.dataset.editId;
  if (id) {
    const key = data.replaceKey === 'on' ? ($('.key-mode.active').dataset.keyMode === 'manual' ? data.manualKey : data.generatedKey) : undefined;
    try { const result = await api(`/api/access-keys/${id}`, { method: 'PATCH', body: JSON.stringify({ name: data.keyName, enabled: data.enabled === 'on', monthlyDataGB: Number(event.target.elements.monthlyDataGB.value || 0), usageSubscriptionId: Number(data.usageSubscriptionId || 0), ...(key ? { key } : {}) }) }); closeAccessModal(); await refreshWorkspace(); if (result.key) showAccessKeyResult(result.key, 'Replacement key ready'); else { showToast(); $('.toast strong').textContent = 'Access key updated'; $('.toast p').textContent = 'The access key changes are saved.'; } } catch (error) { alert(error.message); }
    return;
  }
  const manual = $('.key-mode.active').dataset.keyMode === 'manual';
  const key = manual ? data.manualKey : data.generatedKey;
  try { const result = await api('/api/access-keys', { method: 'POST', body: JSON.stringify({ name: data.keyName, key, monthlyDataGB: Number(event.target.elements.monthlyDataGB.value || 0), usageSubscriptionId: Number(data.usageSubscriptionId || 0) }) }); closeAccessModal(); await refreshWorkspace(); showAccessKeyResult(result.key, 'Access key ready'); } catch (error) { alert(error.message); }
}, true);

$('#save-settings').addEventListener('click', async () => {
  const data = Object.fromEntries(new FormData($('#settings-form')));
  data.defaultInterval = Number(data.defaultInterval); data.schedulerEnabled = data.schedulerEnabled === 'on';
  try { await api('/api/settings', { method: 'PATCH', body: JSON.stringify({ ...state.settings, ...data }) }); await refreshWorkspace(); showToast(); $('.toast strong').textContent = 'Settings saved'; $('.toast p').textContent = 'Changes are stored in the database.'; } catch (error) { alert(error.message); }
});

$('#account-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  const form = event.currentTarget;
  const data = Object.fromEntries(new FormData(form));
  const message = $('#account-settings-message');
  message.classList.remove('success');
  message.textContent = '';
  if (data.newPassword !== data.confirmNewPassword) {
    message.textContent = 'New passwords do not match.';
    return;
  }
  const submit = form.querySelector('button[type="submit"]');
  submit.disabled = true;
  try {
    state.user = await api('/api/auth/account', { method: 'PATCH', body: JSON.stringify(data) });
    renderUser();
    form.elements.currentPassword.value = '';
    form.elements.newPassword.value = '';
    form.elements.confirmNewPassword.value = '';
    message.classList.add('success');
    message.textContent = data.newPassword ? 'Account updated. Other sessions were signed out.' : 'Username updated.';
    showNotice('Account saved', 'Your sign-in details are updated.');
  } catch (error) {
    message.textContent = error.message;
  } finally {
    submit.disabled = false;
  }
});

$('.more-button').addEventListener('click', (event) => { event.stopPropagation(); const menu = $('#account-menu'); const open = menu.classList.toggle('open'); $('.more-button').setAttribute('aria-expanded', String(open)); });
document.addEventListener('click', (event) => { if (!event.target.closest('.user-card')) { $('#account-menu')?.classList.remove('open'); $('.more-button')?.setAttribute('aria-expanded', 'false'); } });
$('[data-account-settings]').addEventListener('click', () => { $('#account-menu').classList.remove('open'); selectView('settings'); });
$('[data-account-signout]').addEventListener('click', async () => { if (!confirm('Sign out of SubStore?')) return; try { await api('/api/auth/logout', { method: 'POST' }); location.reload(); } catch (error) { alert(error.message); } });
$('#config-access-key-select').addEventListener('change', () => { localStorage.setItem('substore-config-access-key-id', $('#config-access-key-select').value); updateConfigAccessKeyURL(); });
$('#copy-config-access-url').addEventListener('click', async () => { const key = state.accessKeys.find((item) => String(item.id) === $('#config-access-key-select').value); const copied = await copyText(key?.key ? subscriptionURL(key.key) : ''); showNotice(copied ? 'Subscription URL copied' : 'Copy unavailable', copied ? 'Paste it into Clash, Mihomo, or OpenClash.' : 'Replace this access key in Access keys first.'); });

function openNamedModal(backdropId, formSelector) { const backdrop = $(`#${backdropId}`); backdrop.classList.add('open'); backdrop.setAttribute('aria-hidden', 'false'); setTimeout(() => $(formSelector)?.querySelector('input')?.focus(), 50); }
function closeNamedModal(backdropId, formSelector) { $(`#${backdropId}`).classList.remove('open'); $(`#${backdropId}`).setAttribute('aria-hidden', 'true'); const form = $(formSelector); form?.reset(); if (form) form.dataset.editId = ''; }
function buttonWithText(text) { return $$('button').find((button) => button.textContent.trim().includes(text)); }

function ensureSubscriptionFilterFields() {
  const form = $('#subscription-form');
  const grid = form?.querySelector('.form-grid');
  if (!form || !grid || form.elements.matchList) return;
  const createFilterField = (name, label, placeholder, hint) => {
    const wrapper = document.createElement('label');
    wrapper.className = 'field-span-2';
    wrapper.innerHTML = `${label}<textarea name="${name}" rows="3" placeholder="${placeholder}"></textarea><span class="field-hint">${hint}</span>`;
    return wrapper;
  };
  grid.insertBefore(createFilterField('matchList', 'Match list', 'e.g. Japan, Tokyo, premium', 'Only proxies matching at least one keyword are kept. Separate keywords with commas or new lines.'), grid.lastElementChild);
  grid.insertBefore(createFilterField('mismatchList', 'Mismatch list', 'e.g. expired, trial, low quality', 'Matching exclusions always win over the match list.'), grid.lastElementChild);
  $('#subscription-form button[type="submit"]').id = 'subscription-submit';
}

function duplicateSubscriptionName(name) {
  const base = `${name} copy`;
  let candidate = base;
  let suffix = 2;
  const existing = new Set((state.subscriptions || []).map((item) => item.name.toLowerCase()));
  while (existing.has(candidate.toLowerCase())) candidate = `${base} ${suffix++}`;
  return candidate;
}

function openSubscriptionModal(item = null, duplicate = false) {
  ensureSubscriptionFilterFields();
  const form = $('#subscription-form');
  form.reset();
  const userAgentSelect = form.elements.userAgent;
  userAgentSelect.querySelectorAll('[data-custom-user-agent]').forEach((option) => option.remove());
  form.dataset.editId = item && !duplicate ? item.id : '';
  $('#subscription-modal-title').textContent = duplicate ? 'Duplicate subscription' : item ? 'Edit subscription' : 'Add subscription';
  $('#subscription-submit').textContent = duplicate ? 'Create duplicate' : item ? 'Save changes' : 'Add subscription';
  if (item) {
    if (item.userAgent && !Array.from(userAgentSelect.options).some((option) => option.value === item.userAgent)) {
      userAgentSelect.add(new Option(`Existing custom value (${item.userAgent})`, item.userAgent));
      userAgentSelect.lastElementChild.dataset.customUserAgent = 'true';
    }
    Object.entries({ name: item.name, url: item.url, userAgent: item.userAgent, updateMode: item.updateMode, intervalMinutes: item.intervalMinutes, matchList: item.matchList || '', mismatchList: item.mismatchList || '' }).forEach(([key, value]) => { if (form.elements[key]) form.elements[key].value = value; });
    if (duplicate) form.elements.name.value = duplicateSubscriptionName(item.name);
    form.elements.enabled.checked = item.enabled;
  }
  openNamedModal('subscription-modal-backdrop', '#subscription-form');
}

function openRecordModal({ backdrop, formSelector, titleId, submitId, title, addLabel, editLabel, item, values }) {
  const form = $(formSelector);
  const submit = form.querySelector('button[type="submit"]');
  form.reset();
  form.dataset.editId = item?.id || '';
  submit.id = submitId;
  $(`#${titleId}`).textContent = item ? `Edit ${title}` : `Add ${title}`;
  submit.textContent = item ? editLabel : addLabel;
  if (item) Object.entries(values(item)).forEach(([key, value]) => { if (form.elements[key]) form.elements[key].value = value; });
  openNamedModal(backdrop, formSelector);
}

function updateRuleFormOptions(selectedMatch = '', selectedTarget = '', selectedTargetGroupID = 0) {
  const form = $('#rule-form');
  const type = form?.elements.ruleType?.value || 'DOMAIN-SUFFIX';
  const textField = $('#rule-match-text-field');
  const providerField = $('#rule-match-provider-field');
  const matchInput = $('#rule-match-input');
  const providerSelect = $('#rule-provider-match-select');
  const isProviderRule = type === 'RULE-SET';
  if (!form || !textField || !providerField || !matchInput || !providerSelect) return;
  textField.hidden = isProviderRule;
  providerField.hidden = !isProviderRule;
  matchInput.disabled = isProviderRule;
  matchInput.required = !isProviderRule;
  providerSelect.disabled = !isProviderRule;
  providerSelect.required = isProviderRule;
  providerSelect.innerHTML = '<option value="">Choose a rule provider</option>' + (state.providers || []).filter((provider) => provider.enabled).map((provider) => `<option value="${escapeHTML(provider.name)}">${escapeHTML(provider.name)} · ${escapeHTML(provider.behavior || 'classical')}</option>`).join('');
  if (isProviderRule) providerSelect.value = selectedMatch || providerSelect.value;
  const targetChoices = [
    { value: 'DIRECT', label: 'DIRECT' },
    { value: 'REJECT', label: 'REJECT' },
    { value: 'PROXY', label: 'PROXY' },
    ...(state.groups || []).filter((group) => group.enabled).map((group) => ({ value: `group-id:${group.id}`, label: group.name })),
  ].filter((choice, index, choices) => choice.value && choices.findIndex((item) => item.value === choice.value) === index);
  const target = $('#rule-target-select');
  if (target) {
    target.innerHTML = targetChoices.map((choice) => `<option value="${escapeHTML(choice.value)}">${escapeHTML(choice.label)}</option>`).join('');
    const groupValue = selectedTargetGroupID ? `group-id:${selectedTargetGroupID}` : '';
    const legacyGroup = targetChoices.find((choice) => choice.label === selectedTarget && choice.value.startsWith('group-id:'))?.value || '';
    const selectedValue = groupValue || legacyGroup || selectedTarget;
    target.value = targetChoices.some((choice) => choice.value === selectedValue) ? selectedValue : 'DIRECT';
  }
}

$('#rule-type-select')?.addEventListener('change', () => updateRuleFormOptions());

function openRuleModal(item = null) {
  openRecordModal({ backdrop: 'rule-modal-backdrop', formSelector: '#rule-form', titleId: 'rule-modal-title', submitId: 'rule-submit', title: 'routing rule', addLabel: 'Add rule', editLabel: 'Save changes', item, values: (value) => ({ ruleType: value.ruleType, priority: value.priority, match: value.match, providerMatch: value.match }) });
  updateRuleFormOptions(item?.match || '', item?.target || '', item?.targetGroupId || 0);
}
function openProviderModal(item = null) { openRecordModal({ backdrop: 'provider-modal-backdrop', formSelector: '#provider-form', titleId: 'provider-modal-title', submitId: 'provider-submit', title: 'rule provider', addLabel: 'Add rule provider', editLabel: 'Save changes', item, values: (value) => ({ name: value.name, type: value.type || 'http', behavior: value.behavior || 'classical', format: value.format || 'yaml', primaryUrl: value.primaryUrl, backupUrl: value.backupUrl || '', interval: value.interval || 86400 }) }); }

function parseGroupMember(value = '') {
  if (value.startsWith('subscription-id:')) return { type: 'subscription', value };
  if (value.startsWith('group-id:')) return { type: 'group', value };
  if (value.startsWith('subscription:')) return { type: 'subscription', value };
  if (value.startsWith('group:')) return { type: 'group', value };
  if (value.startsWith('proxy-id:')) return { type: 'proxy', value };
  if (value.toUpperCase() === 'DIRECT') return { type: 'direct', value: 'DIRECT' };
  return { type: 'proxy', value };
}
function groupMemberChoices(type, editingGroupID = '', excluded = new Set()) {
  let choices = [];
  if (type === 'subscription') choices = (state.subscriptions || []).filter((item) => item.enabled).map((item) => ({ value: `subscription-id:${item.id}`, label: item.name }));
  else if (type === 'group') choices = (state.groups || []).filter((item) => item.enabled && String(item.id) !== String(editingGroupID)).map((item) => ({ value: `group-id:${item.id}`, label: item.name }));
  else if (type === 'proxy') choices = servers.filter((item) => Number(item.id) > 0 && item.enabled).map((item) => ({ value: `proxy-id:${item.id}`, label: item.name }));
  else choices = [{ value: 'DIRECT', label: 'DIRECT' }];
  return choices.filter((item) => !excluded.has(item.value));
}
function updateGroupMemberValue(row, selectedValue = '') {
  const type = row.querySelector('[data-member-type]').value;
  const valueSelect = row.querySelector('[data-member-value]');
  const editingID = $('#group-form')?.dataset.editId || '';
  const excluded = new Set($$('#group-members .group-member-row').filter((item) => item !== row).map((item) => item.querySelector('[data-member-value]').value).filter(Boolean));
  const choices = groupMemberChoices(type, editingID, excluded);
  valueSelect.innerHTML = choices.length ? choices.map((item) => `<option value="${escapeHTML(item.value)}">${escapeHTML(item.label)}</option>`).join('') : '<option value="">No available items</option>';
  valueSelect.value = choices.some((item) => item.value === selectedValue) ? selectedValue : (choices[0]?.value || '');
}
function refreshGroupMemberChoices() {
  $$('#group-members .group-member-row').forEach((row) => updateGroupMemberValue(row, row.querySelector('[data-member-value]').value));
}
function updateGroupMemberOrderControls() {
  const rows = $$('#group-members .group-member-row');
  rows.forEach((row, index) => {
    row.querySelector('[data-member-up]').disabled = index === 0;
    row.querySelector('[data-member-down]').disabled = index === rows.length - 1;
  });
}
function moveGroupMember(row, direction) {
  const sibling = direction < 0 ? row.previousElementSibling : row.nextElementSibling;
  if (!sibling) return;
  if (direction < 0) sibling.before(row); else sibling.after(row);
  updateGroupMemberOrderControls();
  row.querySelector(direction < 0 ? '[data-member-up]' : '[data-member-down]').focus();
}
function addGroupMember(member = null) {
  const container = $('#group-members');
  if (!container) return;
  const parsed = member ? parseGroupMember(member) : { type: state.subscriptions?.some((item) => item.enabled) ? 'subscription' : 'direct', value: '' };
  const row = document.createElement('div');
  row.className = 'group-member-row';
  row.innerHTML = `<span class="group-member-drag" draggable="true" aria-hidden="true" title="Drag to reorder">${dragIcon}</span><select data-member-type aria-label="Member type"><option value="subscription">Subscription</option><option value="group">Proxy group</option><option value="proxy">Manual proxy</option><option value="direct">DIRECT</option></select><select data-member-value aria-label="Member"></select><span class="group-member-actions"><button type="button" class="icon-action" data-member-up aria-label="Move member up" title="Move up">${moveUpIcon}</button><button type="button" class="icon-action" data-member-down aria-label="Move member down" title="Move down">${moveDownIcon}</button><button type="button" class="icon-action danger" data-member-remove aria-label="Remove member" title="Remove member">${trashIcon}</button></span>`;
  const typeSelect = row.querySelector('[data-member-type]');
  typeSelect.value = parsed.type;
  container.appendChild(row);
  updateGroupMemberValue(row, parsed.value);
  typeSelect.addEventListener('change', () => { updateGroupMemberValue(row); refreshGroupMemberChoices(); });
  row.querySelector('[data-member-value]').addEventListener('change', refreshGroupMemberChoices);
  row.querySelector('[data-member-up]').addEventListener('click', () => moveGroupMember(row, -1));
  row.querySelector('[data-member-down]').addEventListener('click', () => moveGroupMember(row, 1));
  row.querySelector('[data-member-remove]').addEventListener('click', () => { row.remove(); updateGroupMemberOrderControls(); refreshGroupMemberChoices(); });
  row.querySelector('.group-member-drag').addEventListener('dragstart', (event) => { row.classList.add('dragging'); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', 'group-member'); });
  row.querySelector('.group-member-drag').addEventListener('dragend', () => { row.classList.remove('dragging'); updateGroupMemberOrderControls(); });
  updateGroupMemberOrderControls();
  refreshGroupMemberChoices();
}
$('#group-members')?.addEventListener('dragover', (event) => {
  const dragging = $('#group-members .group-member-row.dragging');
  const target = event.target.closest('.group-member-row');
  if (!dragging || !target || dragging === target) return;
  event.preventDefault();
  const rect = target.getBoundingClientRect();
  if (event.clientY < rect.top + rect.height / 2) target.before(dragging); else target.after(dragging);
});
$('#group-members')?.addEventListener('drop', (event) => { event.preventDefault(); updateGroupMemberOrderControls(); });
function selectedGroupMembers() {
  return $$('#group-members .group-member-row').map((row) => row.querySelector('[data-member-value]').value).filter(Boolean);
}
function groupMemberLabel(value) {
  if (value.startsWith('proxy-id:')) {
    const item = servers.find((proxy) => String(proxy.id) === value.slice('proxy-id:'.length));
    return `Proxy: ${item?.name || 'Unknown proxy'}`;
  }
  if (value.startsWith('subscription-id:')) {
    const item = (state.subscriptions || []).find((subscription) => String(subscription.id) === value.slice('subscription-id:'.length));
    return `Subscription: ${item?.name || 'Unknown subscription'}`;
  }
  if (value.startsWith('group-id:')) {
    const item = (state.groups || []).find((group) => String(group.id) === value.slice('group-id:'.length));
    return `Group: ${item?.name || 'Unknown group'}`;
  }
  if (value.startsWith('subscription:')) return `Subscription: ${value.slice('subscription:'.length)}`;
  if (value.startsWith('group:')) return `Group: ${value.slice('group:'.length)}`;
  return value;
}
function openGroupModal(item = null) {
  const form = $('#group-form');
  form.reset();
  form.dataset.editId = item?.id || '';
  $('#group-modal-title').textContent = item ? 'Edit proxy group' : 'Add proxy group';
  const submit = form.querySelector('button[type="submit"]');
  submit.id = 'group-submit';
  submit.textContent = item ? 'Save changes' : 'Add proxy group';
  if (item) { form.elements.name.value = item.name; form.elements.type.value = item.type; }
  $('#group-members').innerHTML = '';
  (item?.proxies || []).forEach(addGroupMember);
  if (!item?.proxies?.length) addGroupMember();
  openNamedModal('group-modal-backdrop', '#group-form');
}
$('#add-group-member')?.addEventListener('click', () => addGroupMember());

function renderSubscriptions(items) {
  const grid = $('.subscription-grid');
  if (!grid) return;
  const share = $('.config-share'); if (share) { let usage = $('#combined-usage'); if (!usage) { usage = document.createElement('p'); usage.id = 'combined-usage'; usage.className = 'field-hint'; share.querySelector('.share-url')?.after(usage); } updateCombinedUsageHint(); }
  grid.innerHTML = items.length ? items.map((item) => { const used = Number(item.usedGB || 0); const total = Number(item.totalGB || 0); const hasTotal = total > 0; const percentage = hasTotal ? Math.min(100, Math.max(0, (used / total) * 100)) : 0; const usage = hasTotal ? `${used.toFixed(2)} / ${total.toFixed(2)} GB` : used > 0 ? `${used.toFixed(2)} GB used` : 'Usage unavailable'; const expiry = item.expireAt ? new Date(item.expireAt).toLocaleDateString() : 'No expiry reported'; const progressAttributes = hasTotal ? `role="progressbar" aria-label="Data usage for ${escapeHTML(item.name)}" aria-valuemin="0" aria-valuemax="${total.toFixed(2)}" aria-valuenow="${used.toFixed(2)}"` : `role="progressbar" aria-label="Data usage unavailable for ${escapeHTML(item.name)}" aria-valuemin="0" aria-valuemax="0" aria-valuenow="0"`; return `<div class="subscription-card"><div class="subscription-card-top"><div class="source-logo ${item.name.includes('家宽') ? 'premium' : 'cheap'}">${escapeHTML(item.name[0] || 'S')}</div><span class="status-pill ${item.lastError ? 'offline' : 'online'}"><i></i>${item.lastError ? 'Needs attention' : item.enabled ? 'Enabled' : 'Disabled'}</span></div><h3>${escapeHTML(item.name)}</h3><div class="subscription-usage"><div class="subscription-usage-label"><span>Data used</span><strong>${escapeHTML(usage)}</strong></div><div class="subscription-progress ${hasTotal ? '' : 'unavailable'}" ${progressAttributes}><span style="width:${percentage.toFixed(2)}%"></span></div></div><div class="subscription-metrics"><span><b>${item.proxyCount || 0}</b> proxies</span><span><b>${escapeHTML(expiry)}</b></span></div><div class="subscription-footer"><span>${item.lastSuccessAt ? `Synced ${escapeHTML(item.lastSuccessAt)}` : 'Not synced yet'}</span><span class="row-actions"><button class="icon-action" data-sub-usage="${item.id}" aria-label="View ${escapeHTML(item.name)} usage details" title="Usage details">${usageIcon}</button><button class="mini-button" data-sub-update="${item.id}">Update</button><button class="mini-button" data-sub-duplicate="${item.id}">Duplicate</button><button class="icon-action" data-sub-edit="${item.id}" aria-label="Edit ${escapeHTML(item.name)}" title="Edit subscription">${editIcon}</button><button class="icon-action danger" data-sub-delete="${item.id}" aria-label="Delete ${escapeHTML(item.name)}" title="Delete subscription">${trashIcon}</button></span></div></div>`; }).join('') : '<div class="empty-preview"><div class="empty-icon">↻</div><h3>No subscriptions yet</h3><p>Add a provider URL to start building your proxy pool.</p><button class="button button-secondary" data-add-subscription>Add your first subscription</button></div>';
  $$('[data-sub-usage]').forEach((button) => button.addEventListener('click', () => openSubscriptionUsage(button.dataset.subUsage)));
  $$('[data-sub-edit]').forEach((button) => button.addEventListener('click', () => openSubscriptionModal(items.find((item) => String(item.id) === button.dataset.subEdit))));
  $$('[data-sub-duplicate]').forEach((button) => button.addEventListener('click', () => openSubscriptionModal(items.find((item) => String(item.id) === button.dataset.subDuplicate), true)));
  $$('[data-sub-update]').forEach((button) => button.addEventListener('click', async () => { button.disabled = true; try { await api(`/api/subscriptions/${button.dataset.subUpdate}/update`, { method: 'POST' }); await refreshWorkspace(); showNotice('Subscription updated', 'The latest snapshot is stored locally.'); } catch (error) { alert(error.message); } finally { button.disabled = false; } }));
  $$('[data-sub-delete]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this subscription?', 'The subscription and its imported proxy snapshot will be permanently removed.'))) return; try { await api(`/api/subscriptions/${button.dataset.subDelete}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
  $('[data-add-subscription]')?.addEventListener('click', () => openSubscriptionModal());
}

function closeSubscriptionUsage() {
  $('#subscription-usage-backdrop').classList.remove('open');
  $('#subscription-usage-backdrop').setAttribute('aria-hidden', 'true');
}

async function openSubscriptionUsage(id) {
  const backdrop = $('#subscription-usage-backdrop');
  const body = $('#subscription-usage-body');
  backdrop.classList.add('open');
  backdrop.setAttribute('aria-hidden', 'false');
  $('#subscription-usage-title').textContent = 'Usage history';
  body.innerHTML = '<div class="usage-history-empty">Loading usage history…</div>';
  try {
    const data = await api(`/api/subscriptions/${id}/usage`);
    const summary = data.summary || {};
    $('#subscription-usage-title').textContent = `${data.subscriptionName} usage`;
    const samples = data.samples || [];
    const rows = samples.map((sample) => { const eventLabel = sample.event === 'baseline' ? 'Baseline' : sample.event === 'plan-renewed' ? 'Plan renewed' : sample.event === 'counter-reset' ? 'Counter reset' : 'Measured'; return `<div class="usage-history-row"><time>${escapeHTML(new Date(sample.collectedAt).toLocaleString())}</time><strong>+${Number(sample.deltaGB || 0).toFixed(2)} GB</strong><span>${Number(sample.usedGB || 0).toFixed(2)} / ${Number(sample.totalGB || 0).toFixed(2)} GB</span><em class="usage-event ${sample.event}">${eventLabel}</em></div>`; }).join('');
    body.innerHTML = `<div class="usage-summary-grid"><div><span>Tracked usage</span><strong>${Number(summary.trackedGB || 0).toFixed(2)} GB</strong></div><div><span>Provider counter</span><strong>${Number(summary.currentUsedGB || 0).toFixed(2)} GB</strong></div><div><span>Samples</span><strong>${Number(summary.sampleCount || 0)}</strong></div></div><p class="usage-history-note">Collected once per unique subscription URL each hour. The first reading is a baseline. Renewals and counter resets start a new segment.</p><div class="usage-history-head"><span>COLLECTED</span><span>INTERVAL</span><span>PROVIDER COUNTER</span><span>EVENT</span></div><div class="usage-history-list">${rows || '<div class="usage-history-empty">No samples yet. The first automatic collection will create the baseline.</div>'}</div>`;
  } catch (error) {
    body.innerHTML = `<div class="usage-history-empty">${escapeHTML(error.message)}</div>`;
  }
}

$('#close-subscription-usage').addEventListener('click', closeSubscriptionUsage);
$('#subscription-usage-backdrop').addEventListener('click', (event) => { if (event.target.id === 'subscription-usage-backdrop') closeSubscriptionUsage(); });

function renderRules(items) {
  const panel = $('.rules-heading')?.parentElement;
  if (!panel) return;
  $$('.rule-row').forEach((row) => row.remove());
  const fragment = document.createDocumentFragment();
  (items || []).forEach((item) => { const row = document.createElement('div'); row.className = 'rule-row'; row.draggable = true; row.dataset.ruleId = item.id; row.dataset.isOverride = String(item.isOverride); row.innerHTML = `<span class="rule-type"><span class="drag-handle" title="Drag to reorder">⠿</span> ${escapeHTML(item.ruleType)}</span><strong>${escapeHTML(item.match)}</strong><span class="route-chip direct-chip">${escapeHTML(item.target)}</span><span class="priority">${item.isOverride ? "Override · " : "Order "}<i>${item.priority}</i></span><span class="row-actions"><button class="icon-action" data-edit-rule="${item.id}" aria-label="Edit routing rule" title="Edit routing rule">${editIcon}</button><button class="icon-action danger" data-rule-delete="${item.id}" aria-label="Delete routing rule" title="Delete routing rule">${trashIcon}</button></span>`; fragment.appendChild(row); });
  panel.appendChild(fragment);
  $$('[data-edit-rule]').forEach((button) => button.addEventListener('click', () => openRuleModal((items || []).find((item) => String(item.id) === button.dataset.editRule))));
  $$('[data-rule-delete]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this routing rule?', 'Traffic will no longer follow this routing rule.'))) return; try { await api(`/api/rules/${button.dataset.ruleDelete}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
  $$('.rule-row').forEach((row) => {
    row.addEventListener('dragstart', (event) => { row.classList.add('dragging'); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', row.dataset.ruleId); });
    row.addEventListener('dragend', () => { row.classList.remove('dragging'); $$('.rule-row').forEach((item) => item.classList.remove('drag-over')); });
    row.addEventListener('dragover', (event) => { event.preventDefault(); if (row.dataset.ruleId !== event.dataTransfer.getData('text/plain')) row.classList.add('drag-over'); });
    row.addEventListener('dragleave', () => row.classList.remove('drag-over'));
    row.addEventListener('drop', async (event) => {
      event.preventDefault();
      row.classList.remove('drag-over');
      const source = $(`.rule-row[data-rule-id="${event.dataTransfer.getData('text/plain')}"]`);
      if (!source || source === row) return;
      if (source.dataset.isOverride !== row.dataset.isOverride) { showNotice('Overrides stay first', 'Reorder overrides among overrides, or ordinary rules among ordinary rules.'); return; }
      const rows = $$('.rule-row');
      if (rows.indexOf(source) < rows.indexOf(row)) row.after(source); else row.before(source);
      const ids = $$('.rule-row').map((item) => Number(item.dataset.ruleId));
      try { await api('/api/rules/reorder', { method: 'PATCH', body: JSON.stringify({ ids }) }); await refreshWorkspace(); showNotice('Rules reordered', 'The generated configuration now follows this order.'); } catch (error) { alert(error.message); await refreshWorkspace(); }
    });
  });
}

function renderServiceRuleGroups(items) {
  const container = $('#service-rule-groups');
  if (!container) return;
  const outboundGroups = (state.groups || []).filter((group) => group.enabled);
  container.innerHTML = items.length ? items.map((item) => `<div class="service-rule-card" data-service-rule-group="${item.id}"><div class="service-rule-card-top"><input name="name" value="${escapeHTML(item.name)}" aria-label="Rule group name" /><span class="service-rule-count">${item.ruleCount} rules</span></div><label>Outbound proxy group<select name="targetGroupId">${outboundGroups.map((group) => `<option value="${group.id}" ${String(group.id) === String(item.targetGroupId) ? 'selected' : ''}>${escapeHTML(group.name)}</option>`).join('')}</select></label><div class="service-rule-actions"><label class="service-rule-toggle" aria-label="Enable ${escapeHTML(item.name)} rule group" title="Enable rule group"><input name="enabled" type="checkbox" ${item.enabled ? 'checked' : ''} /><i class="switch"></i></label><button class="icon-action" data-save-service-group="${item.id}" aria-label="Save ${escapeHTML(item.name)} rule group" title="Save rule group">${saveIcon}</button><button class="icon-action danger" data-delete-service-group="${item.id}" aria-label="Delete ${escapeHTML(item.name)} rule group" title="Delete rule group">${trashIcon}</button></div></div>`).join('') : '<div class="empty-preview"><div class="empty-icon">⌁</div><h3>No service rule groups</h3><p>Individual routing rules below are still applied.</p></div>';
  $$('[data-save-service-group]').forEach((button) => button.addEventListener('click', async () => { const card = button.closest('[data-service-rule-group]'); const payload = { name: card.querySelector('[name="name"]').value, targetGroupId: Number(card.querySelector('[name="targetGroupId"]').value), enabled: card.querySelector('[name="enabled"]').checked }; try { await api(`/api/service-rule-groups/${button.dataset.saveServiceGroup}`, { method: 'PATCH', body: JSON.stringify(payload) }); await refreshWorkspace(); showNotice('Rule group saved', 'All inline destination rules now use the selected outbound.'); } catch (error) { alert(error.message); } }));
  $$('[data-delete-service-group]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this service rule group?', 'All of its inline rules will be removed from the generated configuration.'))) return; try { await api(`/api/service-rule-groups/${button.dataset.deleteServiceGroup}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
}

function renderGroups(items) {
  const grid = $('.group-cards');
  if (!grid) return;
  const layout = localStorage.getItem('substore-group-layout') || 'cards';
  grid.classList.toggle('group-list', layout === 'rows');
  grid.innerHTML = items.length ? items.map((item) => { const labels = (item.proxies || []).map(groupMemberLabel); const allMembers = labels.length ? labels : ['All active proxies', 'DIRECT']; const visible = allMembers.slice(0, 2); const remaining = allMembers.length - visible.length; const memberChips = visible.map((label) => `<span class="group-member-chip" title="${escapeHTML(label)}">${escapeHTML(label)}</span>`).join('') + (remaining > 0 ? `<span class="group-member-chip more" title="${escapeHTML(allMembers.slice(2).join(' · '))}">+${remaining} more</span>` : ''); return `<div class="group-card"><div class="group-card-icon">${item.type === 'select' ? '⌁' : '↻'}</div><div class="group-card-content"><span class="group-type">${escapeHTML(item.type)}</span><h3>${escapeHTML(item.name)}</h3><div class="group-member-preview" aria-label="Members: ${escapeHTML(allMembers.join(', '))}">${memberChips}</div></div><span class="row-actions"><button class="icon-action" data-edit-group="${item.id}" aria-label="Edit ${escapeHTML(item.name)}" title="Edit proxy group">${editIcon}</button><button class="icon-action danger" data-group-delete="${item.id}" aria-label="Delete ${escapeHTML(item.name)}" title="Delete proxy group">${trashIcon}</button></span></div>`; }).join('') : '<div class="empty-preview"><div class="empty-icon">⌁</div><h3>No custom proxy groups yet</h3><p>The generated config will use a default PROXY select group.</p></div>';
  $$('[data-edit-group]').forEach((button) => button.addEventListener('click', () => openGroupModal(items.find((item) => String(item.id) === button.dataset.editGroup))));
  $$('[data-group-delete]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this proxy group?', 'The group will be permanently removed from the generated configuration.'))) return; try { await api(`/api/groups/${button.dataset.groupDelete}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
}

function renderRuleProviders(items) {
  const empty = $('#rule-provider-list');
  if (!empty) return;
  empty.classList.toggle('provider-list', items.length > 0);
  empty.classList.toggle('empty-preview', items.length === 0);
  empty.innerHTML = items.length ? items.map((item) => `<div class="provider-card"><div class="source-logo cheap">⌁</div><div><strong>${escapeHTML(item.name)}</strong><span>${escapeHTML(item.type || 'http')} · ${escapeHTML(item.behavior || 'classical')} · ${escapeHTML(item.format || 'yaml')}</span><small>${escapeHTML(item.primaryUrl)} · ${item.lastError ? escapeHTML(item.lastError) : item.lastUpdateAt ? `Updated ${escapeHTML(item.lastUpdateAt)}` : `Every ${item.interval || 86400}s`}</small></div><button class="mini-button" data-provider-update="${item.id}">Update</button><span class="row-actions"><button class="icon-action" data-edit-provider="${item.id}" aria-label="Edit ${escapeHTML(item.name)}" title="Edit rule provider">${editIcon}</button><button class="icon-action danger" data-provider-delete="${item.id}" aria-label="Delete ${escapeHTML(item.name)}" title="Delete rule provider">${trashIcon}</button></span></div>`).join('') : '<div class="empty-icon">⌁</div><h3>Rule providers are ready to be configured</h3><p>Connect a list such as Loyalsoldier to keep routing rules current.</p><button class="button button-secondary" data-add-provider> Add your first provider </button>';
  $$('[data-edit-provider]').forEach((button) => button.addEventListener('click', () => openProviderModal(items.find((item) => String(item.id) === button.dataset.editProvider))));
  $$('[data-provider-update]').forEach((button) => button.addEventListener('click', async () => { try { await api(`/api/rule-providers/${button.dataset.providerUpdate}/update`, { method: 'POST' }); await refreshWorkspace(); showNotice('Rule provider updated', 'The rule list is cached locally.'); } catch (error) { alert(error.message); } }));
  $$('[data-provider-delete]').forEach((button) => button.addEventListener('click', async () => { if (!(await confirmDelete('Delete this rule provider?', 'The downloaded rule list and provider configuration will be removed.'))) return; try { await api(`/api/rule-providers/${button.dataset.providerDelete}`, { method: 'DELETE' }); await refreshWorkspace(); } catch (error) { showRequestError(error); } }));
  $('[data-add-provider]')?.addEventListener('click', () => openProviderModal());
}

async function refreshConfig() {
  const copyButton = $('#view-config .code-toolbar .text-button');
  copyButton.disabled = true;
  try {
    const yaml = await api('/api/config/preview');
    renderConfigYAML(yaml);
    copyButton.disabled = false;
    return true;
  } catch (error) {
    $('#view-config pre').textContent = 'Configuration could not be loaded. Use Regenerate config to try again.';
    $('.config-quick-links').replaceChildren();
    showRequestError(error);
    return false;
  }
}
function showNotice(title, message) { $('.toast strong').textContent = title; $('.toast p').textContent = message; showToast(); }
function renderClientConfig(settings) {
  const form = $('#client-config-form');
  if (!form) return;
  if (!form.elements.groupLayout) {
    const section = document.createElement('section');
    section.className = 'client-config-section';
    section.innerHTML = '<h2>Display</h2><p>This changes the web interface only; it does not change generated YAML.</p><label>Proxy group layout<select name="groupLayout"><option value="cards">Cards</option><option value="rows">One line</option></select></label>';
    form.append(section);
  }
  const dnsPolicyGroup = form.elements.dnsPolicyGroupId;
  dnsPolicyGroup.innerHTML = '<option value="0">Choose a proxy group</option>' + (state.groups || []).filter((group) => group.enabled).map((group) => `<option value="${group.id}">${escapeHTML(group.name)}</option>`).join('');
  Object.entries(settings).forEach(([key, value]) => {
    const field = form.elements[key];
    if (!field) return;
    field.type === 'checkbox' ? field.checked = value : field.value = value;
  });
  form.elements.groupLayout.value = localStorage.getItem('substore-group-layout') || 'cards';
  const selectedGroup = (state.groups || []).find((group) => String(group.id) === String(settings.dnsPolicyGroupId));
  const rows = [['Mixed port', settings.mixedPort], ['Allow LAN', settings.allowLan ? 'Enabled' : 'Disabled'], ['Mode', settings.mode], ['DNS', settings.dnsEnabled ? `${settings.dnsEnhancedMode} enabled` : 'Disabled'], ['Domestic DNS', settings.dnsNameservers], ['Overseas DNS via', selectedGroup?.name || 'Not configured']];
  $('#client-config-summary').innerHTML = rows.map(([name, value]) => `<div><dt>${escapeHTML(name)}</dt><dd title="${escapeHTML(value)}">${escapeHTML(value)}</dd></div>`).join('');
}
$('#client-config-form').addEventListener('change', (event) => { if (event.target.name !== 'groupLayout') return; localStorage.setItem('substore-group-layout', event.target.value); renderGroups(state.groups); });
$$('#client-config-form .settings-switch').forEach((label) => label.addEventListener('click', (event) => { const input = label.querySelector('input[type="checkbox"]'); if (!input || event.target === input) return; event.preventDefault(); input.checked = !input.checked; input.dispatchEvent(new Event('change', { bubbles: true })); }));
$('#save-client-config').addEventListener('click', async () => { const form = $('#client-config-form'); const data = Object.fromEntries(new FormData(form)); ['allowLan','dnsEnabled','dnsIPv6','dnsUseHosts','dnsFallbackGeoIP'].forEach((key) => { data[key] = form.elements[key].checked; }); data.mixedPort = Number(data.mixedPort); data.dnsPolicyGroupId = Number(data.dnsPolicyGroupId); try { const settings = await api('/api/settings', { method: 'PATCH', body: JSON.stringify({ ...state.settings, ...data }) }); state.settings = settings; renderClientConfig(settings); await refreshConfig(); showNotice('Client configuration saved', 'Generated YAML now uses these client settings.'); } catch (error) { alert(error.message); } });

refreshWorkspace = async function refreshWorkspaceWithAllData() {
  resetRuleLookup();
  const [proxyData, subscriptions, rules, accessKeys, settings, groups, providers, serviceRuleGroups] = await Promise.all([api('/api/proxies'), api('/api/subscriptions'), api('/api/rules'), api('/api/access-keys'), api('/api/settings'), api('/api/groups'), api('/api/rule-providers'), api('/api/service-rule-groups')]);
  servers = proxyData.map((server) => ({ ...server, status: server.enabled ? 'Online' : 'Offline', type: server.id < 0 ? 'Subscription' : 'Manual', latency: server.latency || '—', logo: server.name[0]?.toUpperCase() || 'P', color: server.id < 0 ? 'purple' : 'blue' }));
  state.accessKeys = accessKeys; state.settings = settings; state.groups = groups; state.providers = providers; state.serviceRuleGroups = serviceRuleGroups; state.subscriptions = subscriptions; workspaceSubscriptions = subscriptions;
  renderServers($('#server-search')?.value || ''); renderAccessKeys(); renderSubscriptions(subscriptions); renderRules(rules); renderServiceRuleGroups(serviceRuleGroups); renderGroups(groups); renderRuleProviders(providers); refreshConfig();
  $('.nav-count').textContent = subscriptions.length;
  renderDashboard(proxyData, subscriptions, groups, accessKeys, rules, providers);
  const settingsForm = $('#settings-form'); if (settingsForm) Object.entries(settings).forEach(([key, value]) => { const field = settingsForm.elements[key]; if (!field) return; if (field.type === 'checkbox') field.checked = value; else field.value = value; }); renderClientConfig(settings);
};

document.addEventListener('click', (event) => {
  const target = event.target.closest('button');
  if (!target || target.closest('.modal')) return;
  if (target.id === 'subscription-action' || target.matches('[data-add-subscription]')) { event.preventDefault(); event.stopImmediatePropagation(); openSubscriptionModal(); }
  else if (target.textContent.trim().includes('Add rule provider') || target.matches('[data-add-provider]')) { openProviderModal(); }
  else if (target.textContent.trim().includes('Add rule') && !target.closest('#subscription-form')) { openRuleModal(); }
  else if (target.textContent.trim().includes('Add group')) { openGroupModal(); }

  if (target.textContent.trim() === 'Copy YAML') { copyText($('#view-config pre')?.textContent || '').then((copied) => showNotice(copied ? 'YAML copied' : 'Copy failed', copied ? 'The generated configuration is on your clipboard.' : 'Select the configuration and copy it manually.')); }
  if (target.textContent.trim() === 'Regenerate config') { target.disabled = true; refreshConfig().then((success) => { if (success) showNotice('Config regenerated', 'The latest configuration is ready.'); }).finally(() => { target.disabled = false; }); }

}, true);

const closeModalBindings = [['subscription-modal-backdrop', '#subscription-form', 'close-subscription-modal', 'cancel-subscription-modal'], ['rule-modal-backdrop', '#rule-form', 'close-rule-modal', 'cancel-rule-modal'], ['provider-modal-backdrop', '#provider-form', 'close-provider-modal', 'cancel-provider-modal'], ['group-modal-backdrop', '#group-form', 'close-group-modal', 'cancel-group-modal']];
closeModalBindings.forEach(([backdrop, form, close, cancel]) => { $(`#${close}`).addEventListener('click', () => closeNamedModal(backdrop, form)); $(`#${cancel}`).addEventListener('click', () => closeNamedModal(backdrop, form)); $(`#${backdrop}`).addEventListener('click', (event) => { if (event.target.id === backdrop) closeNamedModal(backdrop, form); }); });

document.addEventListener('submit', async (event) => {
  const form = event.target;
  if (!['subscription-form', 'rule-form', 'provider-form', 'group-form'].includes(form.id)) return;
  event.preventDefault(); event.stopImmediatePropagation();
  const data = Object.fromEntries(new FormData(form));
  try {
    if (form.id === 'subscription-form') { data.enabled = data.enabled === 'on'; data.intervalMinutes = Number(data.intervalMinutes); const id = form.dataset.editId; await api(id ? `/api/subscriptions/${id}` : '/api/subscriptions', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(data) }); closeNamedModal('subscription-modal-backdrop', '#subscription-form'); selectView('subscriptions'); }
    if (form.id === 'rule-form') { data.match = data.ruleType === 'RULE-SET' ? data.providerMatch : data.match; delete data.providerMatch; data.priority = Number(data.priority); data.enabled = true; if (data.target.startsWith('group-id:')) { data.targetGroupId = Number(data.target.slice('group-id:'.length)); data.target = $('#rule-target-select').selectedOptions[0].textContent; } else { data.targetGroupId = 0; } const id = form.dataset.editId; await api(id ? `/api/rules/${id}` : '/api/rules', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(data) }); closeNamedModal('rule-modal-backdrop', '#rule-form'); selectView('rules'); }
    if (form.id === 'provider-form') { data.interval = Number(data.interval); data.enabled = true; data.updateMode = 'manual'; const id = form.dataset.editId; await api(id ? `/api/rule-providers/${id}` : '/api/rule-providers', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(data) }); closeNamedModal('provider-modal-backdrop', '#provider-form'); selectView('rule-providers'); }
    if (form.id === 'group-form') { data.proxies = selectedGroupMembers(); data.enabled = true; const id = form.dataset.editId; await api(id ? `/api/groups/${id}` : '/api/groups', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(data) }); closeNamedModal('group-modal-backdrop', '#group-form'); selectView('groups'); }
    await refreshWorkspace(); showNotice('Saved', 'Your changes are now part of the workspace.');
  } catch (error) { alert(error.message); }
}, true);

document.addEventListener('keydown', (event) => { if (event.key === 'Escape') { closeModalBindings.forEach(([backdrop, form]) => closeNamedModal(backdrop, form)); closeSubscriptionUsage(); } });

const protocolFilter = $('#protocol-filter');
const statusFilter = $('#status-filter');
protocolFilter?.addEventListener('change', (event) => { serverFilters.protocol = event.target.value; renderServers($('#server-search')?.value || ''); });
statusFilter?.addEventListener('change', (event) => { serverFilters.status = event.target.value; renderServers($('#server-search')?.value || ''); });


bootApp();

// Dashboard values come exclusively from the current instance.
function renderDashboard(proxies, subscriptions, groups, keys, rules, providers) {
  const values = [proxies.filter((item) => item.enabled).length, subscriptions.length, groups.length, keys.filter((item) => item.enabled).length];
  const notes = ['Available in your configuration', 'Connected provider sources', 'Selection and failover policies', 'Active private subscription URLs'];
  $$('.stat-card').forEach((card, index) => { card.querySelector('.stat-value').textContent = values[index]; card.querySelector('.stat-meta').textContent = notes[index]; });
  $$('.health-row strong').forEach((node, index) => { node.textContent = [groups.length, rules.length, providers.length][index]; });
  const cheap = subscriptions.find((item) => item.name.includes('光喵')) || subscriptions.find((item) => /cheap/i.test(item.name));
  if (!cheap || !(Number(cheap.totalGB) > 0)) { renderTrafficPlaceholder(); return; }
  const total = Number(cheap.totalGB), used = Number(cheap.usedGB || 0);
  const percent = Math.min(100, Math.max(0, used / total * 100));
  $('.traffic-panel').innerHTML = `<div class="panel-header"><div><h2>Subscription usage</h2><p>${escapeHTML(cheap.name)} · Provider-reported allowance</p></div><span class="telemetry-badge">PROVIDER DATA</span></div><div class="dashboard-usage"><div class="usage-total">${used.toFixed(2)} <small>GB</small></div><p>of ${total.toFixed(2)} GB used</p><div class="subscription-progress" role="progressbar" aria-label="Subscription allowance used" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${percent.toFixed(1)}"><span style="width:${percent}%"></span></div><p>${Math.max(0, total-used).toFixed(2)} GB remaining · ${cheap.lastSuccessAt ? `Updated ${escapeHTML(cheap.lastSuccessAt)}` : 'No successful update yet'}</p></div>`;
}

$('#mobile-menu').addEventListener('click', () => {
  const open = $('#sidebar').classList.toggle('open');
  $('#mobile-menu').setAttribute('aria-expanded', String(open));
  $('#mobile-menu').setAttribute('aria-label', open ? 'Close navigation' : 'Open navigation');
});
document.addEventListener('click', (event) => {
  if (!event.target.closest('#sidebar, #mobile-menu')) { $('#sidebar').classList.remove('open'); $('#mobile-menu').setAttribute('aria-expanded', 'false'); $('#mobile-menu').setAttribute('aria-label', 'Open navigation'); }
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && $('#sidebar').classList.contains('open')) { $('#sidebar').classList.remove('open'); $('#mobile-menu').setAttribute('aria-expanded', 'false'); $('#mobile-menu').setAttribute('aria-label', 'Open navigation'); $('#mobile-menu').focus(); }
});

// Keep keyboard focus inside visible dialogs and return it to the opener.
let dialogOpener = null;
let activeDialog = null;
const dialogObserver = new MutationObserver(() => {
  const next = $$('.modal-backdrop.open').at(-1)?.querySelector('.modal') || null;
  if (next === activeDialog) return;
  if (next && !activeDialog) dialogOpener = document.activeElement;
  activeDialog = next;
  $('.app-shell').inert = Boolean(next) || !$('#auth-screen').hidden;
  if (next) next.querySelector('input:not([type="hidden"]), button, select, textarea')?.focus();
  else if (dialogOpener?.isConnected) { dialogOpener.focus(); dialogOpener = null; }
});
dialogObserver.observe(document.body, { subtree: true, attributes: true, attributeFilter: ['class'] });
document.addEventListener('keydown', (event) => {
  if (event.key !== 'Tab') return;
  const dialog = activeDialog || (!$('#auth-screen').hidden ? $('#auth-screen') : null);
  if (!dialog) return;
  const controls = [...dialog.querySelectorAll('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]')].filter((node) => node.getClientRects().length);
  const first = controls[0], last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
});

function resetRuleLookup() {
  lookupDestination = '';
  lookupRevision++;
  $('#rule-override-form').hidden = true;
  $('#rule-lookup-result').textContent = '';
  $('#rule-lookup-form button').disabled = overrideSaving;
}
$('#rule-lookup-input').addEventListener('input', resetRuleLookup);
$('#rule-lookup-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (overrideSaving) return;
  resetRuleLookup();
  const revision = lookupRevision;
  const input = $('#rule-lookup-input').value;
  const button = event.target.querySelector('button');
  button.disabled = true;
  $('#rule-lookup-result').textContent = 'Checking inline and provider rules…';
  try {
    const result = await api(`/api/rules/lookup?destination=${encodeURIComponent(input)}`);
    if (revision !== lookupRevision) return;
    lookupDestination = result.destination;
    const modeNote = state.settings?.mode && state.settings.mode !== 'rule' ? `Client mode is ${state.settings.mode}; these rules apply only in rule mode. ` : '';
    const providerNote = result.providerRule ? ` Provider entry: ${result.providerRule}.` : '';
    $('#rule-lookup-result').textContent = modeNote + (result.certain
      ? `${result.destination} → ${result.target}. Matched: ${result.rule}.${providerNote}`
      : `${result.destination}: routing is uncertain. First known match: ${result.target} (${result.rule}).${providerNote} Earlier rules require runtime data or a readable provider cache: ${result.unresolved.join('; ')}`);
    $('#rule-override-target').innerHTML = '<option value="DIRECT">DIRECT</option><option value="REJECT">REJECT</option>' + (state.groups || []).filter((group) => group.enabled).map((group) => `<option value="group-id:${group.id}">${escapeHTML(group.name)}</option>`).join('');
    const choice = Array.from($('#rule-override-target').options).find((option) => option.textContent === result.target);
    if (choice) $('#rule-override-target').value = choice.value;
    $('#rule-override-form').hidden = false;
  } catch (error) { if (revision === lookupRevision) $('#rule-lookup-result').textContent = error.message; }
  finally { if (revision === lookupRevision) button.disabled = false; }
});
$('#rule-override-form').addEventListener('submit', async (event) => {
  event.preventDefault();
  if (!lookupDestination || overrideSaving) return;
  const button = event.target.querySelector('button');
  overrideSaving = true;
  button.disabled = true;
  button.textContent = 'Saving…';
  $('#rule-lookup-input').disabled = true;
  $('#rule-lookup-form button').disabled = true;
  $('#rule-override-target').disabled = true;
  let saved = false;
  try {
    await api('/api/rules/override', { method: 'POST', body: JSON.stringify({ destination: lookupDestination, target: $('#rule-override-target').value }) });
    saved = true;
    await refreshWorkspace();
    showNotice('Override saved', 'Refresh your client subscription to apply the new routing rule.');
  } catch (error) {
    if (saved) { resetRuleLookup(); showNotice('Override saved', 'Workspace refresh failed. Reload the page to see the saved rule.'); }
    else showRequestError(error);
  } finally {
    overrideSaving = false;
    button.disabled = false;
    button.textContent = 'Save override';
    $('#rule-lookup-input').disabled = false;
    $('#rule-lookup-form button').disabled = false;
    $('#rule-override-target').disabled = false;
  }
  if (saved) $('#rule-lookup-form').requestSubmit();
});

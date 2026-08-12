const toast = document.querySelector('#toast');
let translations = {};
const fallbackTranslations = {
  saveApply: 'Save & Apply', fancontrolOff: 'Fancontrol Off', restart: 'Restart',
  fancontrolEnabled: 'Fancontrol enabled', fancontrolActive: 'Fancontrol active',
  biosControlEnabled: 'BIOS control enabled', configurationSaved: 'Configuration saved',
  applied: 'Configuration saved and applied to {node}.', stopped: 'Fancontrol stopped. BIOS/hardware control is active.',
  modeChanged: 'Active mode changed to {mode}.', restartRequested: 'Fancontrol service restart requested.'
};
function t(key, params = {}) { return String(translations[key] || fallbackTranslations[key] || key).replace(/\{(\w+)\}/g, (_, name) => params[name] ?? `{${name}}`); }
function applyTranslations() {
  document.querySelectorAll('[data-i18n]').forEach((element) => { if (translations[element.dataset.i18n]) element.textContent = translations[element.dataset.i18n]; });
  document.documentElement.lang = window.MikroTikSharedHeader?.getState?.().language || document.documentElement.lang;
}
async function loadTranslations() {
  const lang = window.MikroTikSharedHeader?.getState?.().language || 'en';
  try { const response = await fetch(`/i18n/${lang}.json`, { cache: 'no-store' }); translations = response.ok ? await response.json() : {}; } catch (_) { translations = {}; }
  applyTranslations();
}
let toastTimer;
function notify(message) { toast.textContent = message; toast.classList.add('show'); clearTimeout(toastTimer); toastTimer = setTimeout(() => toast.classList.remove('show'), 2800); }
document.querySelectorAll('.fan-card input').forEach(input => input.addEventListener('change', event => event.currentTarget.closest('.fan-card').classList.toggle('selected', event.currentTarget.checked)));
document.querySelector('#controlToggle').addEventListener('change', event => { const on = event.currentTarget.checked; document.querySelector('#controlText').textContent = t(on ? 'fancontrolEnabled' : 'biosControlEnabled'); notify(on ? t('fancontrolEnabled') : t('biosControlEnabled')); });
document.querySelector('#activeMode').addEventListener('change', event => notify(t('modeChanged', { mode: event.currentTarget.value })));
function readSettings() {
  return {
    ssh: { host: document.querySelector('#sshHost').value, port: +document.querySelector('#sshPort').value, user: document.querySelector('#sshUser').value, auth_method: document.querySelector('#sshAuthMethod').value, private_key: document.querySelector('#sshKey').value, password: document.querySelector('#sshPassword').value },
    mode: document.querySelector('#activeMode').value,
    enabled: document.querySelector('#controlToggle').checked,
    fans: [...document.querySelectorAll('.fan-card input')].map(input => input.checked),
    curve: [...document.querySelectorAll('.pair')].map(pair => [...pair.querySelectorAll('input')].map(input => +input.value)),
    savedAt: new Date().toISOString()
  };
}
document.querySelector('#testSSHBtn').addEventListener('click', async () => { const status = document.querySelector('#sshStatus'); status.textContent = 'Testing SSH…'; status.className = 'ssh-status pending'; try { const response = await fetch('/api/test-ssh', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(readSettings()) }); const result = await response.json(); if (!response.ok || !result.ok) throw new Error(result.error || 'Connection failed'); status.textContent = 'Connected'; status.className = 'ssh-status success'; notify('SSH connection succeeded.'); } catch (error) { status.textContent = 'Connection failed'; status.className = 'ssh-status error'; notify(error.message); } });
function applySettings(settings) {
  const host = settings.ssh?.host || document.querySelector('#sshHost')?.value || '';
  const subtitle = document.querySelector('#subtitle');
  if (subtitle) subtitle.textContent = host ? `${host} · Fancontrol` : '';
  if (settings.ssh) { document.querySelector('#sshHost').value = settings.ssh.host || ''; document.querySelector('#sshPort').value = settings.ssh.port || 22; document.querySelector('#sshUser').value = settings.ssh.user || 'root'; document.querySelector('#sshAuthMethod').value = settings.ssh.auth_method || 'key'; document.querySelector('#sshKey').value = settings.ssh.private_key || ''; document.querySelector('#sshPassword').value = settings.ssh.password || ''; }
  if (settings.mode) document.querySelector('#activeMode').value = settings.mode;
  if (typeof settings.enabled === 'boolean') document.querySelector('#controlToggle').checked = settings.enabled;
  if (Array.isArray(settings.fans)) document.querySelectorAll('.fan-card input').forEach((input, index) => { input.checked = settings.fans[index] !== false; input.closest('.fan-card').classList.toggle('selected', input.checked); });
  if (Array.isArray(settings.curve)) document.querySelectorAll('.pair').forEach((pair, index) => pair.querySelectorAll('input').forEach((input, point) => { if (settings.curve[index]?.[point] !== undefined) input.value = settings.curve[index][point]; }));
  document.querySelector('#controlText').textContent = t(document.querySelector('#controlToggle').checked ? 'fancontrolEnabled' : 'biosControlEnabled');
}
async function remoteAction(endpoint, data, successMessage) { try { const response = await fetch(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) }); const result = await response.json(); if (!response.ok || !result.ok) throw new Error(result.error || 'remote action failed'); document.querySelector('#saveState').textContent = successMessage; notify(successMessage); return true; } catch (error) { notify(error.message); return false; } }
document.querySelector('#saveBtn').addEventListener('click', async () => { const data = readSettings(); await remoteAction('/api/apply', data, t('configurationSaved')); });
document.querySelector('#offBtn').addEventListener('click', async () => { const data = readSettings(); data.enabled = false; if (await remoteAction('/api/off', data, t('stopped'))) { document.querySelector('#controlToggle').checked = false; document.querySelector('#controlText').textContent = t('biosControlEnabled'); } });
document.querySelector('#restartBtn').addEventListener('click', async () => { await remoteAction('/api/restart', readSettings(), t('restartRequested')); });
fetch('/api/config', { cache: 'no-store' }).then(response => response.ok ? response.json() : {}).then(applySettings).catch(() => {});
const sharedHeader = document.querySelector('[data-mikrotik-header-root]');
sharedHeader?.addEventListener('mikrotik:header-ready', loadTranslations);
sharedHeader?.addEventListener('mikrotik:header-setting-changed', event => { if (event.detail?.key === 'language') loadTranslations(); });
loadTranslations();

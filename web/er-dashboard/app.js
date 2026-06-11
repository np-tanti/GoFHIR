(function () {
  'use strict';

  let currentUser = null;
  let patientCache = {};
  let activeSearch = '';
  let waitTimeInterval = null;

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => document.querySelectorAll(s);

  const loginScreen    = $('#login-screen');
  const dashScreen     = $('#dashboard-screen');
  const loginForm      = $('#login-form');
  const loginError     = $('#login-error');
  const userInfo       = $('#user-info');
  const boardCount     = $('#board-count');
  const logoutBtn      = $('#logout-btn');
  const searchInput    = $('#search-input');
  const searchBtn      = $('#search-btn');
  const refreshBtn     = $('#refresh-btn');
  const checkinBtn     = $('#checkin-btn');
  const checkinModal   = $('#checkin-modal');
  const checkinForm    = $('#checkin-form');
  const checkinError   = $('#checkin-error');
  const checkinPID     = $('#checkin-patient-id');
  const chiefComplaint = $('#chief-complaint');
  const suggestions    = $('#patient-suggestions');
  const vitalsModal    = $('#vitals-modal');
  const vitalsForm     = $('#vitals-form');
  const vitalsError    = $('#vitals-error');
  const vitalsPID      = $('#vitals-patient-id');
  const vitalsDisplay  = $('#vitals-patient-display');
  const toast          = $('#toast');

  const esiCols = {1: $('#esi-1'), 2: $('#esi-2'), 3: $('#esi-3'), 4: $('#esi-4'), 5: $('#esi-5')};

  let toastTimer = null;
  function showToast(msg, type) {
    toast.textContent = msg; toast.className = 'toast ' + type;
    toast.classList.remove('hidden');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast.classList.add('hidden'), 3500);
  }

  function showScreen(s) { $$('.screen').forEach(x => x.classList.remove('active')); s.classList.add('active'); }

  async function apiFetch(path, opts) {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(opts?.headers || {}) },
      ...opts,
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || body.issue?.[0]?.diagnostics || 'Request failed (' + res.status + ')');
    }
    return res.json();
  }

  function broadcast(type, data) {
    const ev = new CustomEvent('triage-update', { detail: { type, data } });
    window.dispatchEvent(ev);
  }

  function formatWaitTime(checkinTime) {
    if (!checkinTime) return '--';
    const now = new Date();
    const checkin = new Date(checkinTime);
    const diffMs = now - checkin;
    const diffMins = Math.floor(diffMs / 60000);
    if (diffMins < 60) return diffMins + 'm';
    const hrs = Math.floor(diffMins / 60);
    const mins = diffMins % 60;
    return hrs + 'h ' + mins + 'm';
  }

  function updateWaitTimes() {
    $$('.wait-time').forEach(el => {
      const pid = el.dataset.pid;
      if (patientCache[pid] && patientCache[pid].checkin_time) {
        el.textContent = formatWaitTime(patientCache[pid].checkin_time);
      }
    });
  }

  loginForm.addEventListener('submit', async (e) => {
    e.preventDefault(); loginError.classList.add('hidden');
    try {
      const data = await apiFetch('/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username: $('#username').value.trim(), password: $('#password').value }),
      });
      currentUser = { id: data.user_id, role: data.role };
      enterDashboard();
    } catch (err) { loginError.textContent = err.message; loginError.classList.remove('hidden'); }
  });

  logoutBtn.addEventListener('click', async () => {
    try { await apiFetch('/auth/logout', { method: 'POST' }); } catch (err) {}
    currentUser = null; document.cookie = 'gofhir-session=; Path=/; Max-Age=-1';
    receiveEventSource && receiveEventSource.close();
    clearInterval(waitTimeInterval);
    showScreen(loginScreen);
  });

  function checkSession() {
    const cookies = document.cookie.split(';').map(c => c.trim());
    if (cookies.some(c => c.startsWith('gofhir-session='))) {
      currentUser = { id: 'session', role: 'nurse' };
      enterDashboard();
    } else { showScreen(loginScreen); }
  }

  function enterDashboard() {
    userInfo.textContent = currentUser.id + ' (' + currentUser.role + ')';
    showScreen(dashScreen);
    connectSSE();
    loadBoard();
    waitTimeInterval = setInterval(updateWaitTimes, 60000);
  }

  let receiveEventSource = null;
  function connectSSE() {
    receiveEventSource && receiveEventSource.close();
    receiveEventSource = new EventSource('/events');
    receiveEventSource.addEventListener('connected', () => showToast('Connected to triage system', 'success'));
    receiveEventSource.addEventListener('checkin', (e) => {
      const d = JSON.parse(e.data); broadcast('checkin', d); renderBoardFromCache();
      showToast(d.patient.patient_name + ' triaged (ESI ' + d.patient.esi + ')', 'success');
    });
    receiveEventSource.addEventListener('checkout', (e) => {
      const d = JSON.parse(e.data); broadcast('checkout', d); renderBoardFromCache();
      showToast(d.patient.patient_name + ' discharged from ED', 'success');
    });
    receiveEventSource.addEventListener('esi-update', (e) => {
      const d = JSON.parse(e.data); broadcast('esi-update', d); renderBoardFromCache();
    });
    receiveEventSource.addEventListener('vitals-recorded', (e) => {
      const d = JSON.parse(e.data); broadcast('vitals-recorded', d); renderBoardFromCache();
    });
    receiveEventSource.onerror = () => {
      showToast('Connection lost, reconnecting...', 'error');
    };
  }

  async function loadBoard() {
    try {
      const boardData = await apiFetch('/triage/board');
      patientCache = {};
      boardData.patients.forEach(p => { patientCache[p.patient_id] = p; });
      activeSearch = '';
      renderBoardFromCache();
    } catch (err) { showToast('Failed to load board: ' + err.message, 'error'); }
  }

  function renderBoardFromCache() {
    const patients = Object.values(patientCache);
    const active = patients.filter(p => !p.checked_out_at);
    boardCount.textContent = active.length + ' active';

    let searchFiltered = active;
    if (activeSearch) {
      const q = activeSearch.toLowerCase();
      searchFiltered = active.filter(p =>
        p.patient_id.toLowerCase().includes(q) ||
        (p.patient_name && p.patient_name.toLowerCase().includes(q))
      );
    }

    Object.values(esiCols).forEach(el => {
      const level = parseInt(el.parentElement.dataset.level);
      el.innerHTML = '';
      const colPatients = searchFiltered.filter(p => p.esi === level);
      const countEl = el.parentElement.querySelector('.esi-count');
      if (countEl) countEl.textContent = colPatients.length;

      if (colPatients.length === 0) {
        el.innerHTML = '<p class="empty-msg">No patients</p>';
      }
      colPatients.forEach(p => { el.appendChild(createPatientCard(p)); });
    });

    updateWaitTimes();
  }

  function createPatientCard(p) {
    const card = document.createElement('div');
    card.className = 'patient-card';
    card.dataset.level = p.esi;
    const esiClass = 'esi-' + p.esi;
    const name = p.patient_name || p.patient_id;
    const waitTime = formatWaitTime(p.checkin_time);

    let html = '<span class="wait-time" data-pid="' + p.patient_id + '">' + waitTime + '</span>';
    html += '<div class="patient-name"><span class="esi-badge ' + esiClass + '">I'.repeat(p.esi) + '</span>' + name + '</div>';
    html += '<div class="patient-meta">' + p.patient_id;
    if (p.gender) html += ' | ' + p.gender.toUpperCase();
    if (p.age) html += ' | ' + p.age + 'y';
    html += '</div>';
    if (p.chief_complaint) html += '<div class="patient-complaint">' + p.chief_complaint + '</div>';
    if (p.vitals && p.vitals.systolic_bp) {
      const v = p.vitals;
      html += '<div class="vitals-summary">BP ' + v.systolic_bp + '/' + v.diastolic_bp + ' HR ' + v.heart_rate + ' SpO2 ' + v.oxygen_sat + '%</div>';
    }
    if (!p.checked_out_at) {
      html += '<div class="patient-actions">';
      html += '<button class="action-btn vitals-btn" data-pid="' + p.patient_id + '">Vitals</button>';
      html += '<button class="action-btn discharge-btn" data-pid="' + p.patient_id + '">Discharge</button>';
      html += '</div>';
      html += '<div class="esi-override">ESI: <select class="esi-select" data-pid="' + p.patient_id + '">';
      for (let i = 1; i <= 5; i++) {
        html += '<option value="' + i + '"' + (i === p.esi ? ' selected' : '') + '>' + i + '</option>';
      }
      html += '</select></div>';
    }
    card.innerHTML = html;

    if (!p.checked_out_at) {
      const esiSelect = card.querySelector('.esi-select');
      if (esiSelect) {
        esiSelect.addEventListener('change', (e) => {
          e.stopPropagation();
          doSetESI(p.patient_id, parseInt(e.target.value));
        });
      }
    }

    if (!p.checked_out_at) {
      const vitalsBtn = card.querySelector('.vitals-btn');
      if (vitalsBtn) {
        vitalsBtn.addEventListener('click', (e) => {
          e.stopPropagation(); openVitalsModal(p.patient_id, name);
        });
      }
      const checkoutBtn = card.querySelector('.checkout-btn');
      if (checkoutBtn) {
        checkoutBtn.addEventListener('click', (e) => {
          e.stopPropagation(); doCheckout(p.patient_id);
        });
      }
    }
    return card;
  }

  async function doCheckout(pid) {
    if (!confirm('Discharge patient from ED?')) return;
    try {
      await apiFetch('/triage/checkout', { method: 'POST', body: JSON.stringify({ patient_id: pid }) });
      showToast('Patient discharged', 'success');
    } catch (err) { showToast('Discharge failed: ' + err.message, 'error'); }
  }

  async function doSetESI(pid, esi) {
    try {
      await apiFetch('/triage/esi', { method: 'POST', body: JSON.stringify({ patient_id: pid, esi }) });
      showToast('ESI updated to ' + esi, 'success');
    } catch (err) { showToast('ESI update failed: ' + err.message, 'error'); }
  }

  function computeESI(v) {
    let score = 3;
    if (v.oxygen_sat < 90 || (v.systolic_bp < 90 && v.heart_rate > 120) || v.resp_rate < 8 || v.resp_rate > 30 || v.heart_rate > 180 || v.heart_rate < 40) {
      score = 1;
    } else if ((v.oxygen_sat >= 90 && v.oxygen_sat < 94) || v.systolic_bp < 100 || v.resp_rate > 24 || v.heart_rate > 140 || v.temperature > 39.5) {
      score = 2;
    } else if (v.oxygen_sat >= 95 && v.heart_rate >= 50 && v.heart_rate <= 100 && v.resp_rate >= 10 && v.resp_rate <= 20 && v.systolic_bp >= 100 && v.systolic_bp < 140 && v.temperature >= 36.0 && v.temperature <= 37.5) {
      score = 5;
    } else if (v.systolic_bp >= 100 && v.systolic_bp < 140 && v.heart_rate >= 60 && v.heart_rate <= 100 && v.resp_rate >= 12 && v.resp_rate <= 20 && v.oxygen_sat >= 95 && v.temperature >= 36.5 && v.temperature <= 38.0) {
      score = 4;
    }
    return Math.max(1, Math.min(5, score));
  }

  window.addEventListener('triage-update', (e) => {
    const { type, data } = e.detail;
    if (data.patient) {
      patientCache[data.patient.patient_id] = data.patient;
      renderBoardFromCache();
    }
  });

  searchBtn.addEventListener('click', () => {
    activeSearch = searchInput.value.trim();
    renderBoardFromCache();
  });
  searchInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') searchBtn.click(); });
  refreshBtn.addEventListener('click', () => { searchInput.value = ''; loadBoard(); });

  checkinBtn.addEventListener('click', () => {
    checkinForm.reset(); checkinError.classList.add('hidden');
    suggestions.classList.add('hidden'); checkinModal.classList.remove('hidden');
  });

  let searchTimeout = null;
  checkinPID.addEventListener('input', () => {
    clearTimeout(searchTimeout);
    const q = checkinPID.value.trim();
    if (q.length < 2) { suggestions.classList.add('hidden'); return; }
    searchTimeout = setTimeout(async () => {
      try {
        const url = '/fhir/patient?name=' + encodeURIComponent(q) + '&_count=5';
        const data = await apiFetch(url);
        const entries = data.entry || [];
        suggestions.innerHTML = '';
        if (entries.length === 0) {
          suggestions.innerHTML = '<div class="suggestion-item" style="color:#999;">No matches - will create new</div>';
          suggestions.classList.remove('hidden');
          return;
        }
        entries.forEach(e => {
          const r = e.resource || e;
          const name = r.name?.[0] ? [r.name[0].given?.[0] || '', r.name[0].family || ''].filter(Boolean).join(' ') : r.id;
          const div = document.createElement('div'); div.className = 'suggestion-item';
          div.innerHTML = '<span class="suggestion-name">' + name + '</span> <span class="suggestion-id">' + r.id + '</span>';
          div.addEventListener('click', () => { checkinPID.value = r.id; suggestions.classList.add('hidden'); });
          suggestions.appendChild(div);
        });
        suggestions.classList.remove('hidden');
      } catch (err) { suggestions.innerHTML = '<div class="suggestion-item" style="color:#999;">Search error</div>'; suggestions.classList.remove('hidden'); }
    }, 300);
  });

  checkinForm.addEventListener('submit', async (e) => {
    e.preventDefault(); checkinError.classList.add('hidden');
    const pid = checkinPID.value.trim();
    const complaint = chiefComplaint.value;
    if (!pid) { checkinError.textContent = 'MRN required'; checkinError.classList.remove('hidden'); return; }
    try {
      const data = await apiFetch('/triage/checkin', {
        method: 'POST', body: JSON.stringify({ patient_id: pid, chief_complaint: complaint || 'Other' }),
      });
      checkinModal.classList.add('hidden');
      showToast(data.patient_name + ' checked in (ESI ' + data.esi + ')', 'success');
    } catch (err) { checkinError.textContent = err.message; checkinError.classList.remove('hidden'); }
  });

  function openVitalsModal(pid, name) {
    vitalsPID.value = pid; vitalsDisplay.textContent = pid + ' - ' + name;
    vitalsError.classList.add('hidden'); vitalsForm.reset();
    vitalsPID.value = pid; vitalsModal.classList.remove('hidden');
    if (patientCache[pid] && patientCache[pid].vitals) {
      const v = patientCache[pid].vitals;
      v.systolic_bp && ($('#systolic').value = v.systolic_bp);
      v.diastolic_bp && ($('#diastolic').value = v.diastolic_bp);
      v.heart_rate && ($('#heart-rate').value = v.heart_rate);
      v.resp_rate && ($('#resp-rate').value = v.resp_rate);
      v.oxygen_sat && ($('#oxygen-sat').value = v.oxygen_sat);
      v.temperature && ($('#temperature').value = v.temperature);
    }
  }

  $$('.close-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const modalId = btn.dataset.modal;
      const el = document.getElementById(modalId);
      if (el) el.classList.add('hidden');
    });
  });
  checkinModal.addEventListener('click', (e) => { if (e.target === checkinModal) checkinModal.classList.add('hidden'); });
  vitalsModal.addEventListener('click', (e) => { if (e.target === vitalsModal) vitalsModal.classList.add('hidden'); });

  vitalsForm.addEventListener('submit', async (e) => {
    e.preventDefault(); vitalsError.classList.add('hidden');
    const pid = vitalsPID.value;
    const obsId = 'obs-' + Date.now();
    const sys = parseInt($('#systolic').value);
    const dia = parseInt($('#diastolic').value);
    const hr = parseInt($('#heart-rate').value);
    const rr = parseInt($('#resp-rate').value);
    const spo2 = parseInt($('#oxygen-sat').value);
    const temp = parseFloat($('#temperature').value);

    const observation = {
      resourceType: 'Observation', id: obsId, status: 'final',
      subject: { reference: 'Patient/' + pid },
      code: { coding: [{ system: 'http://loinc.org', code: '85354-9', display: 'Blood pressure panel' }] },
      component: [
        { code: { coding: [{ system: 'http://loinc.org', code: '8480-6' }] }, valueQuantity: { value: sys, unit: 'mmHg' } },
        { code: { coding: [{ system: 'http://loinc.org', code: '8462-4' }] }, valueQuantity: { value: dia, unit: 'mmHg' } },
        { code: { coding: [{ system: 'http://loinc.org', code: '8867-4' }] }, valueQuantity: { value: hr, unit: '/min' } },
        { code: { coding: [{ system: 'http://loinc.org', code: '9279-1' }] }, valueQuantity: { value: rr, unit: '/min' } },
        { code: { coding: [{ system: 'http://loinc.org', code: '2708-6' }] }, valueQuantity: { value: spo2, unit: '%' } },
        { code: { coding: [{ system: 'http://loinc.org', code: '8310-5' }] }, valueQuantity: { value: temp, unit: 'degC' } },
      ],
    };

    try {
      await apiFetch('/fhir/', {
        method: 'POST', headers: { 'Content-Type': 'application/fhir+json', 'X-Resource-Type': 'observation' },
        body: JSON.stringify(observation),
      });
      const recommendedESI = computeESI({ systolic_bp: sys, diastolic_bp: dia, heart_rate: hr, resp_rate: rr, oxygen_sat: spo2, temperature: temp });
      await apiFetch('/triage/esi', {
        method: 'POST', body: JSON.stringify({ patient_id: pid, esi: recommendedESI }),
      });
      vitalsModal.classList.add('hidden');
      showToast('Vitals documented for ' + pid + ' (ESI ' + recommendedESI + ')', 'success');
    } catch (err) { vitalsError.textContent = err.message; vitalsError.classList.remove('hidden'); }
  });

  checkSession();
})();

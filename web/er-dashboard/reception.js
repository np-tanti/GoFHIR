(function () {
  'use strict';

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => document.querySelectorAll(s);

  const loginScreen    = $('#login-screen');
  const dashScreen     = $('#dashboard-screen');
  const loginForm      = $('#login-form');
  const loginError     = $('#login-error');
  const userInfo       = $('#user-info');
  const logoutBtn      = $('#logout-btn');
  const patientForm    = $('#patient-form');
  const saveBtn        = $('#save-btn');
  const resetBtn       = $('#reset-btn');
  const formStatus     = $('#form-status');
  const preview        = $('#patient-preview');
  const searchID       = $('#search-id');
  const searchBtn      = $('#search-btn');
  const refreshListBtn = $('#refresh-list-btn');
  const patientList    = $('#patient-list');
  const toast          = $('#toast');

  let toastTimer = null;
  function showToast(msg, type) {
    toast.textContent = msg; toast.className = 'toast ' + type;
    toast.classList.remove('hidden');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toast.classList.add('hidden'), 4000);
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

  let currentUser = null;
  logoutBtn.addEventListener('click', async () => {
    try { await apiFetch('/auth/logout', { method: 'POST' }); } catch (err) {}
    currentUser = null; document.cookie = 'gofhir-session=; Path=/; Max-Age=-1';
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
    loadPatientList();
  }

  function buildPatient() {
    const patient = { resourceType: 'Patient' };
    const idVal = $('#patient-id').value.trim();
    if (idVal) patient.id = idVal;
    patient.active = $('#active-status').value !== 'false';
    patient.name = [{
      use: 'official',
      family: $('#family-name').value.trim(),
      given: [$('#given-name').value.trim()],
    }];
    patient.gender = $('#gender').value;
    patient.birthDate = $('#birth-date').value;

    const ms = $('#marital-status').value;
    if (ms) {
      const display = { M: 'Married', S: 'Single', D: 'Divorced', W: 'Widowed' };
      patient.maritalStatus = {
        coding: [{ system: 'http://terminology.hl7.org/CodeSystem/v3-MaritalStatus', code: ms, display: display[ms] }],
        text: display[ms],
      };
    }

    const telecom = [];
    const phone = $('#phone').value.trim();
    if (phone) telecom.push({ system: 'phone', value: phone, use: 'home' });
    const email = $('#email').value.trim();
    if (email) telecom.push({ system: 'email', value: email });
    if (telecom.length > 0) patient.telecom = telecom;

    const addrParts = {
      line: $('#address-line').value.trim(),
      city: $('#city').value.trim(),
      state: $('#state').value.trim(),
      postalCode: $('#postal-code').value.trim(),
      country: $('#country').value.trim(),
    };
    const hasAddr = addrParts.line || addrParts.city || addrParts.state || addrParts.postalCode || addrParts.country;
    if (hasAddr) {
      const addr = { use: 'home' };
      if (addrParts.line) addr.line = [addrParts.line];
      if (addrParts.city) addr.city = addrParts.city;
      if (addrParts.state) addr.state = addrParts.state;
      if (addrParts.postalCode) addr.postalCode = addrParts.postalCode;
      if (addrParts.country) addr.country = addrParts.country;
      patient.address = [addr];
    }

    const ecName = $('#ec-name').value.trim();
    const ecRel = $('#ec-relationship').value.trim();
    const ecPhone = $('#ec-phone').value.trim();
    if (ecName || ecRel || ecPhone) {
      const contact = {};
      if (ecName) contact.name = { text: ecName };
      if (ecRel) contact.relationship = [{ text: ecRel }];
      if (ecPhone) contact.telecom = [{ system: 'phone', value: ecPhone }];
      patient.contact = [contact];
    }

    return patient;
  }

  function renderPreview(patient, id) {
    const displayId = id || patient.id || $('#patient-id').value;
    const name = (patient.name?.[0]?.given?.[0] || '') + ' ' + (patient.name?.[0]?.family || '');
    const msMap = { M: 'Married', S: 'Single', D: 'Divorced', W: 'Widowed' };
    const ms = $('#marital-status').value;
    const phone = $('#phone').value.trim();
    const email = $('#email').value.trim();

    let html = '<h3 style="color:var(--primary);margin-bottom:8px;">' + name.trim() + '</h3>';
    html += '<div class="preview-row"><span class="p-label">ID</span><span class="p-value">' + displayId + '</span></div>';
    html += '<div class="preview-row"><span class="p-label">Status</span><span class="p-value">' + (patient.active !== false ? 'Active' : 'Inactive') + '</span></div>';
    html += '<div class="preview-row"><span class="p-label">Gender</span><span class="p-value">' + patient.gender + '</span></div>';
    html += '<div class="preview-row"><span class="p-label">DOB</span><span class="p-value">' + $('#birth-date').value + '</span></div>';
    if (ms) html += '<div class="preview-row"><span class="p-label">Marital</span><span class="p-value">' + (msMap[ms] || ms) + '</span></div>';
    if (phone || email) {
      html += '<div class="preview-section-title">Contact</div>';
      if (phone) html += '<div class="preview-row"><span class="p-label">Phone</span><span class="p-value">' + phone + '</span></div>';
      if (email) html += '<div class="preview-row"><span class="p-label">Email</span><span class="p-value">' + email + '</span></div>';
    }
    preview.innerHTML = html;
  }

  function updatePreview() {
    const patient = buildPatient();
    renderPreview(patient, null);
  }

  ['input', 'change'].forEach(evt => {
    patientForm.querySelectorAll('input, select').forEach(el => {
      el.addEventListener(evt, updatePreview);
    });
  });

  patientForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    formStatus.textContent = ''; formStatus.className = 'form-status';
    const patient = buildPatient();
    if (!patient.name?.[0]?.family || !patient.name?.[0]?.given?.[0] || !patient.id || !patient.birthDate) {
      formStatus.textContent = 'Please fill in all required fields (*)'; formStatus.className = 'form-status error';
      return;
    }
    saveBtn.disabled = true; saveBtn.textContent = 'Saving...';
    try {
      const data = await apiFetch('/fhir/', {
        method: 'POST',
        headers: { 'Content-Type': 'application/fhir+json', 'X-Resource-Type': 'Patient' },
        body: JSON.stringify(patient),
      });
      formStatus.textContent = 'Patient ' + data.id + ' created successfully';
      formStatus.className = 'form-status success';
      showToast('Patient ' + data.id + ' registered', 'success');
      loadPatientList();
    } catch (err) {
      formStatus.textContent = 'Error: ' + err.message;
      formStatus.className = 'form-status error';
    } finally {
      saveBtn.disabled = false; saveBtn.textContent = 'Save Patient';
    }
  });

  resetBtn.addEventListener('click', () => {
    patientForm.reset();
    preview.innerHTML = '<p class="muted">Fill in the form to see a preview.</p>';
    formStatus.textContent = ''; formStatus.className = 'form-status';
  });

  async function loadPatientList() {
    try {
      const data = await apiFetch('/fhir/patient');
      const entries = data.entry || [];
      if (entries.length === 0) {
        patientList.innerHTML = '<p class="muted">No patients found.</p>';
        return;
      }
      patientList.innerHTML = '';
      entries.slice(0, 20).forEach(e => {
        const r = e.resource || e;
        const name = (r.name?.[0]?.given?.[0] || '') + ' ' + (r.name?.[0]?.family || '');
        const div = document.createElement('div'); div.className = 'patient-list-item';
        div.innerHTML = '<span><strong>' + name.trim() + '</strong><br><small>' + r.id + '</small></span>';
        div.style.cursor = 'pointer';
        div.addEventListener('click', () => {
          $('#patient-id').value = r.id;
          if (r.name?.[0]?.given) $('#given-name').value = r.name[0].given[0] || '';
          if (r.name?.[0]?.family) $('#family-name').value = r.name[0].family || '';
          if (r.gender) $('#gender').value = r.gender;
          if (r.birthDate) $('#birth-date').value = r.birthDate;
          updatePreview();
          formStatus.textContent = 'Loaded patient ' + r.id; formStatus.className = 'form-status';
        });
        patientList.appendChild(div);
      });
    } catch (err) {
      patientList.innerHTML = '<p class="muted">Failed to load: ' + err.message + '</p>';
    }
  }

  searchBtn.addEventListener('click', async () => {
    const id = searchID.value.trim();
    if (!id) return;
    try {
      const data = await apiFetch('/fhir/patient?_id=' + encodeURIComponent(id));
      const entries = data.entry || [];
      if (entries.length === 0) {
        patientList.innerHTML = '<p class="muted">No patient found for "' + id + '".</p>';
        return;
      }
      patientList.innerHTML = '';
      entries.forEach(e => {
        const r = e.resource || e;
        const name = (r.name?.[0]?.given?.[0] || '') + ' ' + (r.name?.[0]?.family || '');
        const div = document.createElement('div'); div.className = 'patient-list-item';
        div.innerHTML = '<span><strong>' + name.trim() + '</strong><br><small>' + r.id + '</small></span>';
        div.style.cursor = 'pointer';
        div.addEventListener('click', () => {
          $('#patient-id').value = r.id;
          if (r.name?.[0]?.given) $('#given-name').value = r.name[0].given[0] || '';
          if (r.name?.[0]?.family) $('#family-name').value = r.name[0].family || '';
          if (r.gender) $('#gender').value = r.gender;
          if (r.birthDate) $('#birth-date').value = r.birthDate;
          updatePreview();
          formStatus.textContent = 'Loaded patient ' + r.id; formStatus.className = 'form-status';
        });
        patientList.appendChild(div);
      });
    } catch (err) {
      patientList.innerHTML = '<p class="muted">Search failed: ' + err.message + '</p>';
    }
  });

  refreshListBtn.addEventListener('click', loadPatientList);
  searchID.addEventListener('keydown', (e) => { if (e.key === 'Enter') searchBtn.click(); });

  checkSession();
})();
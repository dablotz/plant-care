/* PlantCare frontend — vanilla JS, no dependencies */

/* ── State ───────────────────────────────────────── */
let currentPlan = null;
let imageUploadMode = 'direct'; // 'direct' | 's3', resolved on init

/* ── DOM refs ────────────────────────────────────── */
const identifyCard   = document.getElementById('identify-card');
const careCard       = document.getElementById('care-card');
const calendarCard   = document.getElementById('calendar-card');
const libraryList    = document.getElementById('library-list');
const libraryEmpty   = document.getElementById('library-empty');
const libraryError   = document.getElementById('library-error');
const savePlantBtn   = document.getElementById('save-plant-btn');
const saveToast      = document.getElementById('save-toast');

const tabs           = document.querySelectorAll('.tab');
const tabName        = document.getElementById('tab-name');
const tabImage       = document.getElementById('tab-image');

const plantNameInput = document.getElementById('plant-name');
const dropZone       = document.getElementById('drop-zone');
const fileInput      = document.getElementById('file-input');
const previewImg     = document.getElementById('preview-img');

const identifyBtn    = document.getElementById('identify-btn');
const identifyError  = document.getElementById('identify-error');

const gotoCalBtn     = document.getElementById('goto-calendar-btn');
const startDateInput = document.getElementById('start-date');
const taskOverrides  = document.getElementById('task-overrides');
const downloadIcsBtn = document.getElementById('download-ics-btn');
const googleLinksBtn = document.getElementById('google-links-btn');
const googleOutput   = document.getElementById('google-links-output');
const googleList     = document.getElementById('google-links-list');
const calendarError  = document.getElementById('calendar-error');

/* ── Tabs ────────────────────────────────────────── */
tabs.forEach(tab => {
  tab.addEventListener('click', () => {
    tabs.forEach(t => t.classList.remove('active'));
    tab.classList.add('active');
    if (tab.dataset.tab === 'name') {
      tabName.classList.remove('hidden');
      tabImage.classList.add('hidden');
    } else {
      tabName.classList.add('hidden');
      tabImage.classList.remove('hidden');
    }
  });
});

/* ── Image Upload / Drop ─────────────────────────── */
let imageFile = null;

dropZone.addEventListener('click', (e) => {
  if (e.target !== fileInput) fileInput.click();
});

dropZone.addEventListener('dragover', (e) => {
  e.preventDefault();
  dropZone.classList.add('dragover');
});

dropZone.addEventListener('dragleave', () => dropZone.classList.remove('dragover'));

dropZone.addEventListener('drop', (e) => {
  e.preventDefault();
  dropZone.classList.remove('dragover');
  const file = e.dataTransfer.files[0];
  if (file) setImageFile(file);
});

fileInput.addEventListener('change', () => {
  if (fileInput.files[0]) setImageFile(fileInput.files[0]);
});

function setImageFile(file) {
  imageFile = file;
  const url = URL.createObjectURL(file);
  previewImg.src = url;
  previewImg.classList.remove('hidden');
}

/* ── Identify ────────────────────────────────────── */
identifyBtn.addEventListener('click', async () => {
  hideError(identifyError);

  const activeTab = document.querySelector('.tab.active').dataset.tab;
  const hasName   = plantNameInput.value.trim() !== '';
  const hasImage  = imageFile !== null;

  if (activeTab === 'name' && !hasName) {
    showError(identifyError, 'Please enter a plant name.');
    return;
  }
  if (activeTab === 'image' && !hasImage) {
    showError(identifyError, 'Please select or drop a photo.');
    return;
  }

  setLoading(identifyBtn, true);

  try {
    let plan;
    if (activeTab === 'image') {
      if (imageUploadMode === 's3') {
        // Step 1: get pre-signed PUT URL
        const urlRes = await fetch(`/api/upload-url?content_type=${encodeURIComponent(imageFile.type || 'image/jpeg')}`);
        const { upload_url, key } = await handleAPIResponse(urlRes);

        // Step 2: upload directly to S3
        const putRes = await fetch(upload_url, {
          method: 'PUT',
          body: imageFile,
          headers: { 'Content-Type': imageFile.type || 'image/jpeg' },
        });
        if (!putRes.ok) throw new Error(`S3 upload failed: ${putRes.status}`);

        // Step 3: identify using the S3 key
        const body = { image_s3_key: key };
        if (hasName) body.name = plantNameInput.value.trim();
        const res = await fetch('/api/identify', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        plan = await handleAPIResponse(res);
      } else {
        const formData = new FormData();
        formData.append('image', imageFile);
        if (hasName) formData.append('name', plantNameInput.value.trim());

        const res = await fetch('/api/identify', { method: 'POST', body: formData });
        plan = await handleAPIResponse(res);
      }
    } else {
      const res = await fetch('/api/identify', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: plantNameInput.value.trim() }),
      });
      plan = await handleAPIResponse(res);
    }

    currentPlan = plan;
    renderCarePlan(plan);
    careCard.classList.remove('hidden');
    careCard.scrollIntoView({ behavior: 'smooth', block: 'start' });

  } catch (err) {
    showError(identifyError, err.message);
  } finally {
    setLoading(identifyBtn, false);
  }
});

/* ── Render Care Plan ────────────────────────────── */
function renderCarePlan(plan) {
  document.getElementById('plant-common-name').textContent =
    plan.common_name || plan.plant_name;
  document.getElementById('plant-scientific-name').textContent =
    plan.scientific_name || '';
  document.getElementById('plant-summary').textContent = plan.summary || '';

  // Care grid
  const grid = document.getElementById('care-grid');
  grid.innerHTML = '';
  const attrs = [
    { label: 'Light', value: plan.light },
    { label: 'Humidity', value: plan.humidity },
    { label: 'Temperature', value: plan.temperature },
    { label: 'Soil', value: plan.soil_type },
  ];
  attrs.forEach(({ label, value }) => {
    if (!value) return;
    const el = document.createElement('div');
    el.className = 'care-item';
    el.innerHTML = `<div class="care-item-label">${label}</div>
                    <div class="care-item-value">${escHtml(value)}</div>`;
    grid.appendChild(el);
  });

  // Pro tips
  const tipsSection = document.getElementById('pro-tips-section');
  const tipsList    = document.getElementById('pro-tips-list');
  tipsList.innerHTML = '';
  if (plan.pro_tips && plan.pro_tips.length > 0) {
    plan.pro_tips.forEach(tip => {
      const li = document.createElement('li');
      li.textContent = tip;
      tipsList.appendChild(li);
    });
    tipsSection.classList.remove('hidden');
  } else {
    tipsSection.classList.add('hidden');
  }

  // Toxicity
  const toxSection = document.getElementById('toxicity-section');
  const toxText    = document.getElementById('toxicity-text');
  if (plan.toxicity_note) {
    toxText.textContent = plan.toxicity_note;
    toxSection.classList.remove('hidden');
  } else {
    toxSection.classList.add('hidden');
  }

  // Schedule rows
  const schedList = document.getElementById('schedule-list');
  schedList.innerHTML = '';
  (plan.schedule || []).forEach(item => {
    const row = document.createElement('div');
    row.className = 'schedule-row';
    const freqLabel = item.frequency_days === 1
      ? 'every day'
      : `every ${item.frequency_days} days`;
    row.innerHTML = `
      <span class="schedule-task">${escHtml(item.task)}</span>
      <span class="schedule-freq">${freqLabel}</span>
      ${item.notes ? `<span class="schedule-notes">${escHtml(item.notes)}</span>` : ''}
    `;
    schedList.appendChild(row);
  });
}

/* ── Go to Calendar ──────────────────────────────── */
gotoCalBtn.addEventListener('click', () => {
  if (!currentPlan) return;
  renderTaskOverrides(currentPlan);

  // Default start date to today
  const today = new Date().toISOString().split('T')[0];
  startDateInput.value = today;

  calendarCard.classList.remove('hidden');
  calendarCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
});

function renderTaskOverrides(plan) {
  taskOverrides.innerHTML = '';
  (plan.schedule || []).forEach(item => {
    const row = document.createElement('div');
    row.className = 'override-row';
    row.innerHTML = `
      <label class="override-label" for="override-${escHtml(item.task)}">${escHtml(item.task)}</label>
      <input type="number"
             id="override-${escHtml(item.task)}"
             data-task="${escHtml(item.task)}"
             value="${item.frequency_days}"
             min="0"
             max="365"
             step="1" />
      <span class="override-unit">days (0 = skip)</span>
    `;
    taskOverrides.appendChild(row);
  });
}

function buildCalendarRequest() {
  const overrides = {};
  taskOverrides.querySelectorAll('input[data-task]').forEach(input => {
    overrides[input.dataset.task] = parseInt(input.value, 10) || 0;
  });
  return {
    care_plan:      currentPlan,
    start_date:     startDateInput.value || new Date().toISOString().split('T')[0],
    task_overrides: overrides,
  };
}

/* ── Download .ics ───────────────────────────────── */
downloadIcsBtn.addEventListener('click', async () => {
  hideError(calendarError);
  setLoading(downloadIcsBtn, true);
  try {
    const res = await fetch('/api/calendar/ics', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(buildCalendarRequest()),
    });

    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: 'Unknown error' }));
      throw new Error(err.error || res.statusText);
    }

    const blob = await res.blob();
    const url  = URL.createObjectURL(blob);
    const a    = document.createElement('a');
    a.href     = url;
    const name = (currentPlan.common_name || currentPlan.plant_name || 'plant')
      .toLowerCase().replace(/\s+/g, '-');
    a.download = `${name}-care.ics`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

  } catch (err) {
    showError(calendarError, err.message);
  } finally {
    setLoading(downloadIcsBtn, false);
  }
});

/* ── Google Calendar Links ───────────────────────── */
googleLinksBtn.addEventListener('click', async () => {
  hideError(calendarError);
  setLoading(googleLinksBtn, true);
  try {
    const res   = await fetch('/api/calendar/google-links', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(buildCalendarRequest()),
    });
    const links = await handleAPIResponse(res);

    googleList.innerHTML = '';
    Object.entries(links).forEach(([task, url]) => {
      // Guard against protocol injection — only allow https: URLs.
      try {
        if (new URL(url).protocol !== 'https:') return;
      } catch { return; }
      const a = document.createElement('a');
      a.href    = url;
      a.target  = '_blank';
      a.rel     = 'noopener noreferrer';
      a.className = 'gcal-link';
      a.textContent = `📅 Add "${task}" to Google Calendar`;
      googleList.appendChild(a);
    });

    googleOutput.classList.remove('hidden');
    googleOutput.scrollIntoView({ behavior: 'smooth', block: 'start' });

  } catch (err) {
    showError(calendarError, err.message);
  } finally {
    setLoading(googleLinksBtn, false);
  }
});

/* ── Plant Library ───────────────────────────────── */
savePlantBtn.addEventListener('click', async () => {
  if (!currentPlan) return;
  setLoading(savePlantBtn, true);
  try {
    const res = await fetch('/api/plants', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ care_plan: currentPlan }),
    });
    await handleAPIResponse(res);
    showToast();
    loadLibrary();
  } catch (err) {
    // surface the error inline on the care card
    const errEl = document.getElementById('identify-error');
    showError(errEl, 'Could not save: ' + err.message);
  } finally {
    setLoading(savePlantBtn, false);
  }
});

function showToast() {
  saveToast.classList.remove('hidden');
  setTimeout(() => saveToast.classList.add('hidden'), 2500);
}

async function loadLibrary() {
  hideError(libraryError);
  try {
    const res     = await fetch('/api/plants');
    const entries = await handleAPIResponse(res);

    libraryList.innerHTML = '';
    if (!entries || entries.length === 0) {
      libraryEmpty.classList.remove('hidden');
      return;
    }
    libraryEmpty.classList.add('hidden');

    entries.forEach(entry => {
      const plan = entry.care_plan;
      const name = plan.common_name || plan.plant_name || 'Unknown plant';
      const date = new Date(entry.created_at).toLocaleDateString(undefined, {
        year: 'numeric', month: 'short', day: 'numeric',
      });

      const row = document.createElement('div');
      row.className = 'library-row';
      row.innerHTML = `
        <div class="library-info">
          <span class="library-name">${escHtml(name)}</span>
          <span class="library-date">${escHtml(date)}</span>
        </div>
        <div class="library-actions">
          <button class="btn-lib-load" data-id="${escHtml(entry.id)}">Load</button>
          <button class="btn-lib-delete" data-id="${escHtml(entry.id)}" aria-label="Delete">✕</button>
        </div>
      `;
      libraryList.appendChild(row);
    });

    libraryList.querySelectorAll('.btn-lib-load').forEach(btn => {
      btn.addEventListener('click', () => {
        const entry = entries.find(e => e.id === btn.dataset.id);
        if (!entry) return;
        currentPlan = entry.care_plan;
        renderCarePlan(entry.care_plan);
        careCard.classList.remove('hidden');
        careCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
      });
    });

    libraryList.querySelectorAll('.btn-lib-delete').forEach(btn => {
      btn.addEventListener('click', () => deletePlant(btn.dataset.id));
    });

  } catch (err) {
    // 503 means storage isn't configured — hide the library card silently
    if (err.message && err.message.includes('storage not configured')) {
      document.getElementById('library-card').classList.add('hidden');
    } else {
      showError(libraryError, 'Could not load library: ' + err.message);
    }
  }
}

async function deletePlant(id) {
  try {
    const res = await fetch(`/api/plants/${id}`, { method: 'DELETE' });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      throw new Error(data.error || `Server error: ${res.status}`);
    }
    loadLibrary();
  } catch (err) {
    showError(libraryError, 'Could not delete: ' + err.message);
  }
}

/* ── Utilities ───────────────────────────────────── */
async function handleAPIResponse(res) {
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || `Server error: ${res.status}`);
  }
  return data;
}

function setLoading(btn, loading) {
  const text    = btn.querySelector('.btn-text');
  const spinner = btn.querySelector('.btn-spinner');
  btn.disabled = loading;
  if (text)    text.classList.toggle('hidden', loading);
  if (spinner) spinner.classList.toggle('hidden', !loading);
}

function showError(el, msg) {
  el.textContent = msg;
  el.classList.remove('hidden');
}

function hideError(el) {
  el.textContent = '';
  el.classList.add('hidden');
}

function escHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/* ── Init ────────────────────────────────────────── */
async function init() {
  try {
    const res = await fetch('/api/config');
    const cfg = await res.json();
    if (cfg.image_upload_mode) imageUploadMode = cfg.image_upload_mode;
  } catch (_) {
    // keep default 'direct'
  }
  loadLibrary();
}

init();

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
	"github.com/spf13/viper"
	"html/template"
	"net"
	"net/http"
	"strings"
	"time"
)

func maybeEditAutoModePreflight() error {
	mode := rancherMode()
	if mode != "auto" {
		return nil
	}
	if viper.GetBool("rancher.auto_approve") || panelNonInteractiveMode() {
		return nil
	}

	configPath := strings.TrimSpace(viper.ConfigFileUsed())
	if configPath == "" {
		return fmt.Errorf("failed to determine tool-config.yml path for preflight editor")
	}

	versions := currentPreflightVersions()
	if len(versions) == 0 {
		versions = []string{""}
	}

	if err := editAutoModePreflightWithBrowser(configPath, versions); err != nil {
		return fmt.Errorf("auto-mode preflight editor failed: %w", err)
	}

	return nil
}

func currentPreflightVersions() []string {
	viperConfigMu.RLock()
	defer viperConfigMu.RUnlock()

	requestedVersions := viper.GetStringSlice("rancher.versions")
	if len(requestedVersions) > 0 {
		versions := make([]string, 0, len(requestedVersions))
		for _, version := range requestedVersions {
			versions = append(versions, imagelookup.NormalizeVersionInput(version))
		}
		return versions
	}

	if singleVersion := imagelookup.NormalizeVersionInput(viper.GetString("rancher.version")); singleVersion != "" {
		return []string{singleVersion}
	}

	totalRanchers := configuredRancherInstanceCount()
	if totalRanchers < 1 {
		totalRanchers = 1
	}

	return make([]string, totalRanchers)
}

func editAutoModePreflightWithBrowser(configPath string, versions []string) error {
	token, err := randomConfirmationToken()
	if err != nil {
		return fmt.Errorf("failed to create preflight editor token: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start preflight editor listener: %w", err)
	}

	serverErrCh := make(chan error, 1)
	resultCh := make(chan error, 1)

	initialVersionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("failed to serialize preflight versions: %w", err)
	}
	initialCustomHostname := settings.CurrentCustomHostnamePrefix()
	initialCustomHostnameEnabled := initialCustomHostname != ""

	pageTemplate := template.Must(template.New("preflight-editor").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Rancher Setup Preflight</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f4efe7;
      --panel: rgba(255, 252, 247, 0.96);
      --text: #1d1a16;
      --muted: #665d52;
      --border: rgba(78, 62, 43, 0.18);
      --accent: #1e6a52;
      --accent-strong: #16513f;
      --secondary: rgba(127, 108, 91, 0.14);
      --danger: #9b3b2a;
      --shadow: 0 24px 80px rgba(55, 39, 20, 0.18);
    }

    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #1a1d1c;
        --panel: rgba(34, 38, 37, 0.96);
        --text: #f3efe8;
        --muted: #b7ada2;
        --border: rgba(209, 196, 178, 0.16);
        --accent: #5bc29c;
        --accent-strong: #3ea882;
        --secondary: rgba(127, 108, 91, 0.18);
        --danger: #ff9d8f;
        --shadow: 0 24px 80px rgba(0, 0, 0, 0.4);
      }
    }

    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background:
        radial-gradient(circle at top left, rgba(32, 120, 92, 0.14), transparent 34%),
        radial-gradient(circle at top right, rgba(175, 118, 52, 0.14), transparent 28%),
        linear-gradient(180deg, rgba(255,255,255,0.18), transparent 48%),
        var(--bg);
      color: var(--text);
      display: grid;
      place-items: center;
      padding: 24px;
    }
    .shell {
      width: min(980px, 100%);
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 22px;
      box-shadow: var(--shadow);
      overflow: hidden;
      backdrop-filter: blur(14px);
    }
    .header {
      padding: 24px 28px 12px;
    }
    h1 {
      margin: 0;
      font-size: clamp(1.45rem, 2.2vw, 2rem);
      line-height: 1.15;
    }
    .subtitle {
      margin: 10px 0 0;
      color: var(--muted);
      font-size: 0.98rem;
      max-width: 70ch;
    }
    .body {
      padding: 0 20px 20px;
    }
    .panel {
      border: 1px solid var(--border);
      border-radius: 18px;
      background: rgba(0, 0, 0, 0.03);
      padding: 16px;
    }
    .row-header, .row {
      display: grid;
      grid-template-columns: 100px minmax(0, 1fr) 100px;
      gap: 12px;
      align-items: center;
    }
    .row-header {
      color: var(--muted);
      font-size: 0.8rem;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      padding: 0 6px 8px;
    }
    .rows {
      display: grid;
      gap: 10px;
    }
    .field-help {
      color: var(--muted);
      font-size: 13px;
      font-weight: 650;
      margin: 10px 6px 2px;
    }
    .row {
      padding: 10px 6px;
      border-top: 1px solid var(--border);
    }
    .row:first-child {
      border-top: 0;
    }
    .ha-label {
      font-weight: 800;
    }
    input[type="text"] {
      width: 100%;
      border: 1px solid var(--border);
      background: transparent;
      color: var(--text);
      border-radius: 12px;
      padding: 11px 13px;
      font: inherit;
    }
    .summary {
      margin-top: 14px;
      display: grid;
      gap: 8px;
      color: var(--muted);
      font-size: 0.94rem;
    }
    .summary strong {
      color: var(--text);
    }
    .custom-hostname {
      margin-top: 18px;
      padding: 16px;
      border: 1px solid var(--border);
      border-radius: 14px;
      background: rgba(30, 106, 82, 0.08);
      display: grid;
      gap: 12px;
    }
    .checkbox-row {
      display: flex;
      align-items: flex-start;
      gap: 10px;
      color: var(--text);
      font-weight: 800;
    }
    .checkbox-row input {
      margin-top: 3px;
      width: 18px;
      height: 18px;
      accent-color: var(--accent);
      flex: 0 0 auto;
    }
    .checkbox-row span {
      display: block;
      color: var(--muted);
      font-size: 13px;
      font-weight: 650;
      margin-top: 3px;
    }
    .hostname-row {
      display: grid;
      grid-template-columns: minmax(160px, 1fr) auto;
      gap: 10px;
      align-items: center;
    }
    .hostname-suffix {
      color: var(--muted);
      font-weight: 750;
      overflow-wrap: anywhere;
    }
    .hostname-help {
      color: var(--muted);
      font-size: 13px;
      font-weight: 650;
    }
    .custom-hostname[data-enabled="false"] .hostname-row {
      display: none;
    }
    .custom-hostname[data-enabled="false"] .hostname-help {
      display: none;
    }
    .error {
      margin-top: 14px;
      min-height: 1.4em;
      color: var(--danger);
      font-weight: 600;
    }
    .status {
      margin-top: 10px;
      color: var(--muted);
      font-size: 0.94rem;
      min-height: 1.4em;
    }
    .actions {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      padding: 18px 20px 22px;
      border-top: 1px solid var(--border);
    }
    .left-actions, .right-actions {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
    }
    button {
      appearance: none;
      border: 0;
      border-radius: 999px;
      padding: 11px 18px;
      font: inherit;
      font-weight: 700;
      cursor: pointer;
      transition: transform 120ms ease, opacity 120ms ease, background 120ms ease;
    }
    button:hover { transform: translateY(-1px); }
    button:active { transform: translateY(0); }
    button:disabled {
      opacity: 0.6;
      cursor: default;
      transform: none;
    }
    .secondary {
      background: var(--secondary);
      color: var(--text);
    }
    .continue {
      background: var(--accent);
      color: white;
    }
    .continue:hover {
      background: var(--accent-strong);
    }
    .remove {
      color: var(--danger);
    }
    code {
      font: 12.5px/1.5 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    }
    @media (max-width: 760px) {
      .row-header {
        display: none;
      }
      .row {
        grid-template-columns: 1fr;
      }
    }
  </style>
</head>
<body>
  <div class="shell">
    <div class="header">
      <h1>Rancher Setup Preflight</h1>
      <p class="subtitle">Review the requested Rancher versions or exact custom server images for this run before resolving the final plan. The row count becomes <code>total_has</code> automatically.</p>
    </div>
    <div class="body">
      <div class="panel">
        <div class="row-header">
          <div>HA</div>
          <div>Rancher Version or Exact Image</div>
          <div>Remove</div>
        </div>
        <div class="rows" id="rows"></div>
        <div class="field-help">Examples: 2.14-head, head, or bigkevmcd/rancher:v2.16-da0ab2f1dc-head. Docker Hub shorthand is accepted, and a version-like image tag selects the Rancher line used for Kubernetes compatibility lookup. A leading v on a version selector is stripped when saved; exact image tags are preserved.</div>
        <div class="custom-hostname" id="customHostnameBox" data-enabled="false">
          <label class="checkbox-row">
            <input id="customHostnameToggle" type="checkbox" />
            <div>
              Use a custom Rancher URL
              <span>Creates exactly one HA and uses this DNS label for Rancher. AWS resource names still use <code>aws_prefix</code>.</span>
            </div>
          </label>
          <div class="hostname-row">
            <input id="customHostnameInput" type="text" placeholder="foobar" />
            <div class="hostname-suffix" id="hostnameSuffix"></div>
          </div>
          <div class="hostname-help">Example: type foobar and Rancher will use foobar.the.example.com.</div>
        </div>
        <div class="summary">
          <div><strong>Total HAs for this run:</strong> <span id="totalHasValue"></span></div>
          <div><strong>Config file:</strong> <code>{{.ConfigPath}}</code></div>
          <div><strong>Mode:</strong> auto</div>
          <div>This screen edits <code>rancher.versions</code>, <code>total_has</code>, and the optional custom Rancher hostname. The next step shows the resolved chart, image, and RKE2 plan.</div>
        </div>
        <div class="error" id="errorBox"></div>
        <div class="status" id="statusBox"></div>
      </div>
    </div>
    <div class="actions">
      <div class="left-actions">
        <button class="secondary" id="addBtn" type="button">Add HA</button>
      </div>
      <div class="right-actions">
        <button class="secondary" id="cancelBtn" type="button">Cancel</button>
        <button class="continue" id="continueBtn" type="button">Continue to Plan</button>
      </div>
    </div>
  </div>
  <script>
    const token = {{printf "%q" .Token}};
    let versions = {{.InitialVersionsJSON}};
    let customHostnameEnabled = {{.InitialCustomHostnameEnabled}};
    let customHostname = sanitizeDisplayValue({{printf "%q" .InitialCustomHostname}});
    let saving = false;
    const rowsEl = document.getElementById('rows');
    const totalHasValueEl = document.getElementById('totalHasValue');
    const errorBoxEl = document.getElementById('errorBox');
    const statusBoxEl = document.getElementById('statusBox');
    const addBtnEl = document.getElementById('addBtn');
    const cancelBtnEl = document.getElementById('cancelBtn');
    const continueBtnEl = document.getElementById('continueBtn');
    const customHostnameBoxEl = document.getElementById('customHostnameBox');
    const customHostnameToggleEl = document.getElementById('customHostnameToggle');
    const customHostnameInputEl = document.getElementById('customHostnameInput');
    const hostnameSuffixEl = document.getElementById('hostnameSuffix');

    function escapeHtml(value) {
      return String(value)
        .replaceAll('&', '&amp;')
        .replaceAll('<', '&lt;')
        .replaceAll('>', '&gt;')
        .replaceAll('"', '&quot;');
    }

    function sanitizeDisplayValue(value) {
      let next = String(value || '').trim();
      while (next.length >= 2) {
        const first = next[0];
        const last = next[next.length - 1];
        if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
          next = next.slice(1, -1).trim();
          continue;
        }
        break;
      }
      return next;
    }

    function renderRows() {
      if (customHostnameEnabled && versions.length !== 1) {
        versions = [versions[0] || ''];
      }
      rowsEl.innerHTML = versions.map((version, index) => {
        const removeDisabled = customHostnameEnabled || versions.length === 1 ? ' disabled' : '';
        return (
          '<div class="row">' +
            '<div class="ha-label">HA ' + (index + 1) + '</div>' +
            '<div><input type="text" value="' + escapeHtml(version) + '" data-index="' + index + '" placeholder="2.15.1-rcs-c936 or bigkevmcd/rancher:v2.16-da0ab2f1dc-head" /></div>' +
            '<div><button class="secondary remove" type="button" data-remove-index="' + index + '"' + removeDisabled + '>Remove</button></div>' +
          '</div>'
        );
      }).join('');
      totalHasValueEl.textContent = String(versions.length);
      addBtnEl.disabled = saving || customHostnameEnabled;

      rowsEl.querySelectorAll('input[data-index]').forEach(input => {
        input.addEventListener('input', event => {
          const index = Number(event.target.getAttribute('data-index'));
          versions[index] = event.target.value;
          errorBoxEl.textContent = '';
        });
      });

      rowsEl.querySelectorAll('button[data-remove-index]').forEach(button => {
        button.addEventListener('click', () => {
          if (versions.length === 1 || saving) {
            return;
          }
          if (customHostnameEnabled) {
            return;
          }
          versions.splice(Number(button.getAttribute('data-remove-index')), 1);
          renderRows();
        });
      });
    }

    function renderCustomHostname() {
      customHostnameBoxEl.dataset.enabled = customHostnameEnabled ? 'true' : 'false';
      customHostnameToggleEl.checked = customHostnameEnabled;
      customHostnameInputEl.value = customHostname;
      hostnameSuffixEl.textContent = '.the.example.com';
      renderRows();
    }

    function normalizeVersion(value) {
      const normalized = String(value || '').trim();
      return normalized.includes('/') ? normalized : normalized.replace(/^[vV]/, '');
    }

    function normalizedVersions() {
      return versions.map(version => normalizeVersion(version)).filter((_, index) => true);
    }

    function validateVersions() {
      const trimmed = normalizedVersions();
      if (!trimmed.length) {
        return 'At least one HA version is required.';
      }
      for (let i = 0; i < trimmed.length; i++) {
        if (!trimmed[i]) {
          return 'Version for HA ' + (i + 1) + ' cannot be empty.';
        }
      }
      if (customHostnameEnabled) {
        if (trimmed.length !== 1) {
          return 'Custom Rancher URL can only be used with one HA.';
        }
        if (!String(customHostname || '').trim()) {
          return 'Enter a custom Rancher URL label.';
        }
      }
      return '';
    }

    function setSavingState(nextSaving) {
      saving = nextSaving;
      addBtnEl.disabled = nextSaving || customHostnameEnabled;
      cancelBtnEl.disabled = nextSaving;
      continueBtnEl.disabled = nextSaving;
      customHostnameToggleEl.disabled = nextSaving;
      customHostnameInputEl.disabled = nextSaving;
      rowsEl.querySelectorAll('input, button[data-remove-index]').forEach(el => {
        el.disabled = nextSaving || (el.hasAttribute('data-remove-index') && (customHostnameEnabled || versions.length === 1));
      });
    }

    async function continueToPlan() {
      const validationError = validateVersions();
      if (validationError) {
        errorBoxEl.textContent = validationError;
        return;
      }

      errorBoxEl.textContent = '';
      statusBoxEl.textContent = 'Saving config and resolving Rancher plans...';
      setSavingState(true);

      const response = await fetch('/submit?token=' + encodeURIComponent(token), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          versions: normalizedVersions(),
          customHostnameEnabled,
          customHostname
        })
      });
      if (!response.ok) {
        errorBoxEl.textContent = await response.text();
        statusBoxEl.textContent = '';
        setSavingState(false);
        return;
      }

      document.body.innerHTML = '<div style="min-height:100vh;display:grid;place-items:center;font-family:ui-sans-serif,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:inherit;color:inherit;padding:24px;"><div style="max-width:720px;text-align:center;"><h1 style="margin:0 0 12px;">Config saved</h1><p style="margin:0;color:inherit;opacity:.82;">Continuing to the resolved Rancher plan...</p></div></div>';
    }

    async function cancel() {
      if (saving) {
        return;
      }
      await fetch('/cancel?token=' + encodeURIComponent(token), { method: 'POST' });
      document.body.innerHTML = '<div style="min-height:100vh;display:grid;place-items:center;font-family:ui-sans-serif,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif;background:inherit;color:inherit;padding:24px;"><div style="max-width:720px;text-align:center;"><h1 style="margin:0 0 12px;">Canceled</h1><p style="margin:0;color:inherit;opacity:.82;">The setup run was canceled before plan resolution.</p></div></div>';
    }

    addBtnEl.addEventListener('click', () => {
      if (saving || customHostnameEnabled) {
        return;
      }
      versions.push('');
      renderRows();
    });
    customHostnameToggleEl.addEventListener('change', event => {
      if (saving) {
        return;
      }
      customHostnameEnabled = event.target.checked;
      errorBoxEl.textContent = '';
      renderCustomHostname();
    });
    customHostnameInputEl.addEventListener('input', event => {
      customHostname = event.target.value;
      errorBoxEl.textContent = '';
    });
    cancelBtnEl.addEventListener('click', cancel);
    continueBtnEl.addEventListener('click', continueToPlan);

    renderCustomHostname();
  </script>
</body>
</html>`))

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.URL.Query().Get("token")) != token {
			http.Error(w, "invalid preflight editor token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pageTemplate.Execute(w, struct {
			Token                        string
			ConfigPath                   string
			InitialVersionsJSON          template.JS
			InitialCustomHostnameEnabled bool
			InitialCustomHostname        string
		}{
			Token:                        token,
			ConfigPath:                   configPath,
			InitialVersionsJSON:          template.JS(string(initialVersionsJSON)),
			InitialCustomHostnameEnabled: initialCustomHostnameEnabled,
			InitialCustomHostname:        initialCustomHostname,
		})
	})
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		if !preflightEditorAuthorized(r, token) {
			http.Error(w, "invalid preflight editor token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req settings.PreflightConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		normalizedVersions, err := normalizePreflightVersions(req.Versions)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Versions = normalizedVersions

		if err := updateAutoModeConfigFile(configPath, req); err != nil {
			http.Error(w, fmt.Sprintf("failed to update tool-config.yml: %v", err), http.StatusInternalServerError)
			return
		}

		writeJSON(w, map[string]string{"status": "saved"})
		select {
		case resultCh <- nil:
		default:
		}
	})
	mux.HandleFunc("/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !preflightEditorAuthorized(r, token) {
			http.Error(w, "invalid preflight editor token", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		writeJSON(w, map[string]string{"status": "canceled"})
		select {
		case resultCh <- fmt.Errorf("user canceled Rancher setup preflight editor"):
		default:
		}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			serverErrCh <- serveErr
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	editorURL := fmt.Sprintf("http://%s/?token=%s", listener.Addr().String(), token)
	if err := openBrowser(editorURL); err != nil {
		return fmt.Errorf("failed to open preflight editor page: %w", err)
	}

	select {
	case resultErr := <-resultCh:
		return resultErr
	case serveErr := <-serverErrCh:
		return fmt.Errorf("preflight editor server failed: %w", serveErr)
	case <-time.After(30 * time.Minute):
		return fmt.Errorf("timed out waiting for preflight editor response")
	}
}

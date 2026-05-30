const API_BASE_CANDIDATES = ["/nasnotify/api", "/api"];
let ugToken = null;
let API_BASE = null;

const state = {
  bootstrap: null,
  flash: "",
  gatewayStatus: null
};

function bootstrapSetupToken() {
  return state.bootstrap?.setup_token || "";
}

function resolveApiBase() {
  const currentPath = window.location.pathname || "/";
  const trimmedPath = currentPath.endsWith("/") ? currentPath.slice(0, -1) : currentPath;
  const currentDir = trimmedPath.includes("/") ? trimmedPath.slice(0, trimmedPath.lastIndexOf("/")) : "";
  const lastSegment = trimmedPath.split("/").pop() || "";
  const appRootPath = currentPath.endsWith("/") || !lastSegment.includes(".") ? trimmedPath : currentDir;
  const pathCandidates = [];
  if (appRootPath) {
    pathCandidates.push(`${appRootPath}/api`);
  }
  if (currentDir && currentDir !== appRootPath) {
    pathCandidates.push(`${currentDir}/api`);
  }
  if (currentPath.includes("/nasnotify")) {
    pathCandidates.push("/nasnotify/api");
  }
  pathCandidates.push("/api");

  for (const base of [API_BASE, ...pathCandidates, ...API_BASE_CANDIDATES]) {
    const normalized = String(base || "").trim();
    if (normalized) {
      return normalized;
    }
  }
  return API_BASE_CANDIDATES[0];
}

function wechatQRCodeImageSrc() {
  return `${resolveApiBase()}/wechat/qrcode?t=${Date.now()}`;
}

function appRoot() {
  return document.getElementById("app");
}

function escapeHtml(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function getSdkCandidates() {
  const candidates = [];
  if (typeof window === "undefined") {
    return candidates;
  }

  if (window.ugSdk) {
    candidates.push(window.ugSdk);
  }

  for (const target of [window.parent, window.top]) {
    if (!target || target === window) {
      continue;
    }
    try {
      if (target.ugSdk) {
        candidates.push(target.ugSdk);
      }
    } catch (_) {
      // Ignore cross-origin frame access errors.
    }
  }

  return candidates.filter(Boolean);
}

async function resolveUgToken() {
  if (ugToken) {
    return ugToken;
  }

  const sdk = getSdkCandidates().find((candidate) => typeof candidate?.getUgInfo === "function");
  if (!sdk) {
    return null;
  }

  try {
    const token = await new Promise((resolve) => {
      let settled = false;

      const finish = (value) => {
        if (!settled) {
          settled = true;
          resolve(value || null);
        }
      };

      sdk.getUgInfo((error, info) => {
        if (error) {
          finish(null);
          return;
        }
        finish(info && info.third_token ? String(info.third_token) : null);
      });

      window.setTimeout(() => finish(null), 1200);
    });

    if (token) {
      ugToken = token;
    }
    return ugToken;
  } catch (_) {
    return null;
  }
}

function apiBaseCandidates() {
  const values = [];
  const seen = new Set();
  const currentPath = window.location.pathname || "/";
  const trimmedPath = currentPath.endsWith("/") ? currentPath.slice(0, -1) : currentPath;
  const currentDir = trimmedPath.includes("/") ? trimmedPath.slice(0, trimmedPath.lastIndexOf("/")) : "";
  const lastSegment = trimmedPath.split("/").pop() || "";
  const appRootPath = currentPath.endsWith("/") || !lastSegment.includes(".") ? trimmedPath : currentDir;
  const pathCandidates = [
    `${appRootPath || ""}/api`,
    `${currentDir || ""}/api`
  ];

  for (const base of [API_BASE, ...pathCandidates, ...API_BASE_CANDIDATES]) {
    const normalized = String(base || "").trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    values.push(normalized);
  }
  return values;
}

async function fetchWithTimeout(url, options = {}, timeoutMs = 12000) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);

  try {
    return await fetch(url, {
      ...options,
      signal: controller.signal
    });
  } catch (error) {
    if (error?.name === "AbortError") {
      throw new Error(`请求超时（${timeoutMs}ms）`);
    }
    throw error;
  } finally {
    window.clearTimeout(timer);
  }
}

async function api(path, options = {}) {
  let lastError = null;

  for (const base of apiBaseCandidates()) {
    const headers = {
      "Content-Type": "application/json",
      ...(options.headers || {})
    };

    if (ugToken) {
      headers["Ugreen-Ttk"] = ugToken;
    }

    try {
      const response = await fetchWithTimeout(`${base}${path}`, {
        credentials: "same-origin",
        headers,
        ...options
      });

      let data = null;
      const contentType = response.headers.get("content-type") || "";
      if (contentType.includes("application/json")) {
        try {
          data = await response.json();
        } catch (error) {
          throw new Error(`响应 JSON 解析失败：${error.message}`);
        }
      } else {
        const text = await response.text();
        data = text ? { message: text } : null;
      }

      if (!response.ok) {
        const error = new Error((data && (data.error || data.message)) || "请求失败");
        error.status = response.status;
        throw error;
      }

      API_BASE = base;
      return data;
    } catch (error) {
      lastError = error;
      if (error.status && error.status !== 404) {
        throw error;
      }
    }
  }

  throw lastError || new Error("无法连接 NasNotify 服务");
}

function currentView() {
  if (!state.bootstrap) {
    return "loading";
  }
  if (!state.bootstrap.initialized) {
    return "setup";
  }
  if (!state.bootstrap.authenticated) {
    return "login";
  }
  return "dashboard";
}

async function loadBootstrap() {
  await resolveUgToken();
  state.bootstrap = await api("/bootstrap", { method: "GET" });
}

async function loadGatewayStatus() {
  if (!state.bootstrap?.authenticated) {
    state.gatewayStatus = null;
    return;
  }

  try {
    state.gatewayStatus = await api("/wechat/status", { method: "GET" });
  } catch (error) {
    state.gatewayStatus = {
      configured: false,
      open_api_ready: false,
      entry_bound: false,
      bound: false,
      activated: false,
      binding_code: "",
      tips: [],
      last_error: error.message
    };
  }
}

function shellTemplate({ title, subtitle, sideTitle, sideText, body, actionBar = "" }) {
  return `
    <div class="shell">
      <aside class="hero">
        <div class="hero-badge">UGREEN Native App</div>
        <h1>${escapeHtml(sideTitle)}</h1>
        <p>${escapeHtml(sideText)}</p>
        <div class="hero-list">
          <div class="hero-item"><span>1</span><div>应用安装在 NAS 内部，只负责当前这一台绿联 NAS。</div></div>
          <div class="hero-item"><span>2</span><div>微信交互改为本地微信网关，不再依赖 PushPlus 或企业微信应用。</div></div>
          <div class="hero-item"><span>3</span><div>用户扫码进入微信入口，再发送绑定码，后续即可接收通知，并响应查询与风扇/CPU 控制指令。</div></div>
        </div>
        <div class="hero-version">${escapeHtml(state.bootstrap?.version || "")}</div>
      </aside>
      <main class="panel">
        <header class="panel-head">
          <div>
            <div class="eyebrow">NasNotify</div>
            <h2>${escapeHtml(title)}</h2>
            <p>${escapeHtml(subtitle)}</p>
          </div>
          ${actionBar}
        </header>
        <section class="panel-body">${body}</section>
      </main>
    </div>
  `;
}

function renderLoading() {
  appRoot().innerHTML = `
    <div class="loading-screen">
      <div class="loading-card">
        <div class="spinner"></div>
        <h1>NasNotify</h1>
        <p>正在连接 NAS 应用服务，请稍候。</p>
      </div>
    </div>
  `;
}

function renderError(message) {
  appRoot().innerHTML = `
    <div class="loading-screen">
      <div class="loading-card error-card">
        <h1>应用加载失败</h1>
        <p>${escapeHtml(message)}</p>
        <button class="primary-btn" id="retryBtn">重新加载</button>
      </div>
    </div>
  `;
  document.getElementById("retryBtn").addEventListener("click", bootstrapApp);
}

function renderNotice(id) {
  return `<div id="${id}" class="notice error hidden"></div>`;
}

function baseConfigForm(config, includeAdminPassword) {
  return `
    <section class="section-card">
      <div class="section-head">
        <h3>运行设置</h3>
        <span>单机模式</span>
      </div>
      <div class="grid two">
        ${includeAdminPassword ? `
          <label class="field">
            <span>新管理员密码</span>
            <input type="password" id="new_admin_password" placeholder="留空表示不修改">
          </label>
        ` : ""}
        <label class="field">
          <span>通知轮询间隔（分钟）</span>
          <input type="number" id="interval_minutes" min="0.1" step="0.1" value="${escapeHtml(config.interval_minutes || 5)}">
        </label>
        <label class="field">
          <span>系统状态推送间隔（分钟）</span>
          <input type="number" id="system_status_interval_minutes" min="1" step="1" value="${escapeHtml(config.system_status_interval_minutes || 60)}">
        </label>
        <label class="field field-wide">
          <span>本机 NAS 显示名称</span>
          <input type="text" id="local_nas_name" value="${escapeHtml(config.local_nas_name || "本机绿联 NAS")}" placeholder="例如：客厅 NAS">
        </label>
        <label class="field">
          <span>本机 NAS 端口</span>
          <input type="number" id="local_nas_port" min="1" step="1" value="${escapeHtml(config.local_nas_port || 9999)}" placeholder="默认 9999">
        </label>
        <label class="field">
          <span>本机 NAS 管理账号</span>
          <input type="text" id="local_nas_username" value="${escapeHtml(config.local_nas_username || "")}" placeholder="用于读取系统状态">
        </label>
        <label class="field">
          <span>本机 NAS 管理密码</span>
          <input type="password" id="local_nas_password" value="${escapeHtml(config.local_nas_password || "")}" placeholder="留空表示保持现有值">
        </label>
      </div>
    </section>
  `;
}

function gatewayStatusMarkup() {
  const status = state.gatewayStatus || {};
  const qr = status.qrcode || null;
  const needVerifyCode = Boolean(status.need_verify_code);
  const hasQRCode = Boolean(qr?.url || qr?.qrcode);
  const loginButtonText = hasQRCode ? "重新生成二维码" : "生成二维码";
  const qrMarkup = qr?.url
    ? `<img class="qr-image" src="${escapeHtml(wechatQRCodeImageSrc())}" alt="QR code" referrerpolicy="no-referrer">`
    : qr?.qrcode
      ? `<div class="qr-fallback">${escapeHtml(qr.qrcode)}</div>`
      : `<div class="qr-placeholder">保存配置后，这里会显示微信登录二维码或文本码。</div>`;

  const tips = Array.isArray(status.tips) ? status.tips : [];
  const bindState = status.bound ? "已完成绑定" : "等待发送绑定码";

  return `
    <section class="section-card">
      <div class="section-head">
        <h3>微信网关绑定</h3>
        <span>${escapeHtml(bindState)}</span>
      </div>
      <div class="grid two">
        <label class="field field-wide">
          <span>本地微信网关地址</span>
          <input type="text" id="wechat_gateway_url" value="${escapeHtml(state.bootstrap?.config?.wechat_gateway_url || "http://127.0.0.1:5091")}" placeholder="例如：http://127.0.0.1:5091">
        </label>
        <label class="field field-wide">
          <span>网关共享密钥</span>
          <input type="password" id="wechat_gateway_secret" value="${escapeHtml(state.bootstrap?.config?.wechat_gateway_secret || "")}" placeholder="可选，用于保护本地网关接口">
        </label>
      </div>
      <div class="binding-layout">
        <div class="qr-box">${qrMarkup}</div>
        <div class="binding-panel">
          <div class="status-pills">
            <span class="pill ${status.configured ? "ok" : ""}">配置${status.configured ? "已完成" : "未完成"}</span>
            <span class="pill ${status.open_api_ready ? "ok" : ""}">网关${status.open_api_ready ? "在线" : "离线"}</span>
            <span class="pill ${status.entry_bound ? "ok" : ""}">登录${status.entry_bound ? "已进入" : "未进入"}</span>
            <span class="pill ${status.bound ? "ok" : ""}">绑定${status.bound ? "已完成" : "待匹配"}</span>
          </div>
          ${status.last_error ? `<div class="notice warm">${escapeHtml(status.last_error)}</div>` : ""}
          <div class="binding-code-card">
            <div class="binding-code-label">当前绑定码</div>
            <div class="binding-code-value" id="bindingCodeValue">${escapeHtml(status.binding_code || "------")}</div>
            <button type="button" class="ghost-btn" id="copyBindingCodeBtn">复制绑定码</button>
          </div>
          <div class="steps-card">
            <div class="step-item"><strong>第 1 步：</strong>点击生成二维码，并使用微信扫描左侧二维码完成登录。</div>
            <div class="step-item"><strong>第 2 步：</strong>扫码登录后，在微信端先随意发送一条消息用于激活会话。</div>
            <div class="step-item"><strong>第 3 步：</strong>再向该入口发送当前绑定码，等待页面显示已绑定。</div>
            <div class="step-item"><strong>第 4 步：</strong>绑定完成后，可发送 菜单、状态、通知、存储、Docker、进程、备份、电源、UPS、测试、风扇2、CPU1 等固定指令。</div>
          </div>
          ${needVerifyCode ? `
            <div class="verify-card">
              <label class="field">
                <span>手机微信数字验证码</span>
                <input type="text" id="wechat_verify_code" placeholder="输入扫码后微信里显示的数字">
              </label>
              <button type="button" class="ghost-btn" id="submitVerifyCodeBtn">提交验证码</button>
            </div>
          ` : ""}
          ${status.entry_bind_time ? `<div class="meta-line">微信登录时间：${escapeHtml(status.entry_bind_time)}</div>` : ""}
          ${status.bind_time ? `<div class="meta-line">NasNotify 绑定时间：${escapeHtml(status.bind_time)}</div>` : ""}
          <div class="tips-list">
            ${tips.map((tip) => `<div class="tip-item">${escapeHtml(tip)}</div>`).join("")}
          </div>
        </div>
      </div>
      <div class="inline-actions">
        <button type="button" class="ghost-btn" id="startGatewayLoginBtn">${loginButtonText}</button>
        <button type="button" class="ghost-btn" id="refreshGatewayBtn">刷新绑定状态</button>
        <button type="button" class="ghost-btn danger-btn" id="unbindGatewayBtn">解绑并重置绑定码</button>
      </div>
    </section>
  `;
}

function setupBody(config) {
  return `
    <form id="setupForm" class="form-stack">
      ${state.flash ? `<div class="notice success">${escapeHtml(state.flash)}</div>` : ""}
      ${renderNotice("setupError")}
      <div class="notice warm">初始化密钥由后端安全生成，只在首次初始化页面显示。应用只保留本机绿联 NAS 和本地微信网关通道。</div>
      <section class="section-card">
        <div class="section-head">
          <h3>管理员初始化</h3>
          <span>必填</span>
        </div>
        <div class="grid two">
          <label class="field field-wide">
            <span>初始化密钥</span>
            <input type="text" id="init_token" value="${escapeHtml(bootstrapSetupToken())}" placeholder="后端生成的初始化密钥" readonly>
          </label>
          <label class="field">
            <span>管理员密码</span>
            <input type="password" id="admin_password" placeholder="至少 8 位">
          </label>
          <label class="field">
            <span>确认管理员密码</span>
            <input type="password" id="admin_password_confirm" placeholder="再次输入密码">
          </label>
        </div>
      </section>
      ${baseConfigForm(config, false)}
      <section class="section-card">
        <div class="section-head">
          <h3>微信网关配置</h3>
          <span>初始化后可直接扫码绑定</span>
        </div>
        <div class="grid two">
          <label class="field field-wide">
            <span>本地微信网关地址</span>
            <input type="text" id="wechat_gateway_url" value="${escapeHtml(config.wechat_gateway_url || "http://127.0.0.1:5091")}" placeholder="例如：http://127.0.0.1:5091">
          </label>
          <label class="field field-wide">
            <span>网关共享密钥</span>
            <input type="password" id="wechat_gateway_secret" value="${escapeHtml(config.wechat_gateway_secret || "")}" placeholder="可选，用于保护本地网关接口">
          </label>
        </div>
        <div class="notice warm">如果微信网关和应用部署在同一台 NAS，建议使用默认地址 <code>http://127.0.0.1:5091</code>。</div>
      </section>
      <div class="footer-actions">
        <button type="submit" class="primary-btn">完成初始化</button>
      </div>
    </form>
  `;
}

function loginBody() {
  return `
    <form id="loginForm" class="form-stack compact">
      ${state.flash ? `<div class="notice success">${escapeHtml(state.flash)}</div>` : ""}
      ${renderNotice("loginError")}
      <section class="section-card">
        <div class="section-head">
          <h3>进入控制台</h3>
          <span>认证</span>
        </div>
        <label class="field">
          <span>管理员密码</span>
          <input type="password" id="login_password" placeholder="请输入管理员密码">
        </label>
      </section>
      <div class="footer-actions">
        <button type="submit" class="primary-btn">登录</button>
      </div>
    </form>
  `;
}

function dashboardBody(config) {
  return `
    <form id="dashboardForm" class="form-stack">
      ${state.flash ? `<div class="notice success">${escapeHtml(state.flash)}</div>` : ""}
      ${renderNotice("dashboardError")}
      ${baseConfigForm(config, true)}
      ${gatewayStatusMarkup()}
      <div class="footer-actions split">
        <button type="button" class="ghost-btn" id="testPushBtn">发送测试通知</button>
        <button type="submit" class="primary-btn">保存并应用</button>
      </div>
    </form>
  `;
}

function renderApp() {
  const view = currentView();
  const config = state.bootstrap?.config || {};

  if (view === "setup") {
    appRoot().innerHTML = shellTemplate({
      title: "初始化 NasNotify",
      subtitle: "先完成管理员初始化和本机 NAS 配置，随后接入本地微信网关。",
      sideTitle: "单机通知应用",
      sideText: "这个版本只保留本机绿联 NAS 场景，并把微信交互改为本地微信网关加绑定码匹配。",
      body: setupBody(config)
    });
    bindSetup();
    return;
  }

  if (view === "login") {
    appRoot().innerHTML = shellTemplate({
      title: "登录控制台",
      subtitle: "输入管理员密码继续。",
      sideTitle: "微信网关版",
      sideText: "登录后可以直接看到二维码、绑定码、网关状态和固定指令绑定流程。",
      body: loginBody()
    });
    bindLogin();
    return;
  }

  appRoot().innerHTML = shellTemplate({
    title: "本机绿联 NAS 管理",
    subtitle: "应用只管理当前 NAS，微信入口通过本地微信网关接入。",
    sideTitle: "固定指令机器人",
    sideText: "扫码登录微信入口，再发送绑定码完成绑定。绑定成功后，这个入口会接收系统通知，并响应菜单、状态、风扇2、CPU1 等固定指令。",
    actionBar: `<button type="button" class="ghost-btn" id="logoutBtn">退出登录</button>`,
    body: dashboardBody(config)
  });
  bindDashboard();
  bindGatewayActions("dashboardError");
  document.getElementById("logoutBtn").addEventListener("click", handleLogout);
}

function showFormError(id, message) {
  const box = document.getElementById(id);
  if (!box) {
    return;
  }
  box.textContent = message;
  box.classList.remove("hidden");
  box.scrollIntoView({ behavior: "smooth", block: "center" });
}

function clearFormError(id) {
  const box = document.getElementById(id);
  if (!box) {
    return;
  }
  box.textContent = "";
  box.classList.add("hidden");
}

function collectConfig() {
  return {
    interval_minutes: Number(document.getElementById("interval_minutes").value) || 5,
    system_status_interval_minutes: Number(document.getElementById("system_status_interval_minutes").value) || 60,
    local_nas_name: document.getElementById("local_nas_name").value.trim(),
    local_nas_port: Number(document.getElementById("local_nas_port").value) || 9999,
    local_nas_username: document.getElementById("local_nas_username").value.trim(),
    local_nas_password: document.getElementById("local_nas_password").value,
    wechat_gateway_url: document.getElementById("wechat_gateway_url").value.trim(),
    wechat_gateway_secret: document.getElementById("wechat_gateway_secret").value.trim()
  };
}

async function copyText(text) {
  if (!text) {
    return;
  }
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const input = document.createElement("textarea");
  input.value = text;
  document.body.appendChild(input);
  input.select();
  document.execCommand("copy");
  document.body.removeChild(input);
}

function bindGatewayActions(errorId) {
  const startBtn = document.getElementById("startGatewayLoginBtn");
  const refreshBtn = document.getElementById("refreshGatewayBtn");
  const unbindBtn = document.getElementById("unbindGatewayBtn");
  const copyBtn = document.getElementById("copyBindingCodeBtn");
  const verifyBtn = document.getElementById("submitVerifyCodeBtn");

  if (copyBtn) {
    copyBtn.addEventListener("click", async () => {
      try {
        await copyText(state.gatewayStatus?.binding_code || "");
        state.flash = "绑定码已复制，可以直接发送到微信入口。";
        renderApp();
      } catch (error) {
        showFormError(errorId, error.message || "复制失败");
      }
    });
  }

  if (startBtn) {
    startBtn.addEventListener("click", async () => {
      clearFormError(errorId);
      try {
        await api("/wechat/login/start", { method: "POST", body: "{}" });
        state.flash = "新的二维码已生成，请使用微信扫码。";
        await loadGatewayStatus();
        renderApp();
      } catch (error) {
        showFormError(errorId, error.message);
      }
    });
  }

  if (refreshBtn) {
    refreshBtn.addEventListener("click", async () => {
      clearFormError(errorId);
      try {
        await loadGatewayStatus();
        state.flash = "绑定状态已刷新。";
        renderApp();
      } catch (error) {
        showFormError(errorId, error.message);
      }
    });
  }

  if (unbindBtn) {
    unbindBtn.addEventListener("click", async () => {
      clearFormError(errorId);
      try {
        await api("/wechat/unbind", { method: "POST", body: "{}" });
        state.flash = "微信入口已解绑，并已生成新的绑定码。";
        await loadGatewayStatus();
        renderApp();
      } catch (error) {
        showFormError(errorId, error.message);
      }
    });
  }

  if (verifyBtn) {
    verifyBtn.addEventListener("click", async () => {
      clearFormError(errorId);
      try {
        const code = document.getElementById("wechat_verify_code")?.value?.trim() || "";
        await api("/wechat/login/verify-code", {
          method: "POST",
          body: JSON.stringify({ verify_code: code })
        });
        state.flash = "验证码已提交，请稍候刷新状态。";
        await loadGatewayStatus();
        renderApp();
      } catch (error) {
        showFormError(errorId, error.message);
      }
    });
  }
}

function bindSetup() {
  document.getElementById("setupForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    clearFormError("setupError");

    const initToken = document.getElementById("init_token").value.trim();
    const password = document.getElementById("admin_password").value;
    const confirmPassword = document.getElementById("admin_password_confirm").value;
    const config = collectConfig();

    if (!initToken) {
      showFormError("setupError", "请填写初始化密钥。");
      return;
    }
    if (password.length < 8) {
      showFormError("setupError", "管理员密码至少 8 位。");
      return;
    }
    if (password !== confirmPassword) {
      showFormError("setupError", "两次输入的管理员密码不一致。");
      return;
    }
    if (!config.local_nas_username) {
      showFormError("setupError", "请填写本机 NAS 管理账号。");
      return;
    }
    if (!config.local_nas_password) {
      showFormError("setupError", "首次初始化请填写本机 NAS 管理密码。");
      return;
    }
    if (!config.wechat_gateway_url) {
      showFormError("setupError", "请填写本地微信网关地址。");
      return;
    }

    try {
      await api("/setup", {
        method: "POST",
        body: JSON.stringify({
          init_token: initToken,
          admin_password: password,
          config
        })
      });
      state.flash = "初始化完成。登录后即可扫码并使用绑定码完成微信入口绑定。";
      await bootstrapApp();
    } catch (error) {
      showFormError("setupError", error.message);
    }
  });
}

function bindLogin() {
  document.getElementById("loginForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    clearFormError("loginError");

    try {
      await api("/login", {
        method: "POST",
        body: JSON.stringify({
          password: document.getElementById("login_password").value
        })
      });
      state.flash = "登录成功。";
      await bootstrapApp();
    } catch (error) {
      showFormError("loginError", error.message);
    }
  });
}

function bindDashboard() {
  document.getElementById("dashboardForm").addEventListener("submit", async (event) => {
    event.preventDefault();
    clearFormError("dashboardError");

    const config = collectConfig();
    const newPassword = document.getElementById("new_admin_password").value;

    if (newPassword && newPassword.length < 8) {
      showFormError("dashboardError", "新管理员密码至少 8 位。");
      return;
    }
    if (!config.local_nas_username) {
      showFormError("dashboardError", "请填写本机 NAS 管理账号。");
      return;
    }
    if (!config.wechat_gateway_url) {
      showFormError("dashboardError", "请填写本地微信网关地址。");
      return;
    }

    try {
      await api("/save", {
        method: "POST",
        body: JSON.stringify({
          new_admin_password: newPassword,
          config
        })
      });
      state.flash = "配置已保存。";
      await bootstrapApp();
    } catch (error) {
      showFormError("dashboardError", error.message);
    }
  });

  document.getElementById("testPushBtn").addEventListener("click", async () => {
    clearFormError("dashboardError");
    try {
      await api("/test-push", { method: "POST", body: "{}" });
      state.flash = "测试通知已发送，请检查微信入口。";
      await loadGatewayStatus();
      renderApp();
    } catch (error) {
      showFormError("dashboardError", error.message);
    }
  });
}

async function handleLogout() {
  try {
    await api("/logout", { method: "POST", body: "{}" });
  } finally {
    state.flash = "已退出登录。";
    await bootstrapApp();
  }
}

async function bootstrapApp() {
  renderLoading();
  try {
    await loadBootstrap();
    await loadGatewayStatus();
    renderApp();
  } catch (error) {
    renderError(error.message || "无法连接 NasNotify 服务");
  }
}

bootstrapApp();

(function () {
  "use strict";
  var TOKEN = new URLSearchParams(location.search).get("token") || "";
  var state = { config: null, defaultPrompt: "", lastText: "", lastSrt: "", lastPath: "", busy: false, ctrl: null, timer: null, t0: 0 };
  var $ = function (id) { return document.getElementById(id); };

  function api(path, body, signal) {
    var sep = path.indexOf("?") >= 0 ? "&" : "?";
    var opts = { headers: { "X-Bili-Sub-Token": TOKEN } };
    if (body !== undefined) {
      opts.method = "POST";
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    if (signal) opts.signal = signal;
    return fetch(path + sep + "token=" + encodeURIComponent(TOKEN), opts).then(function (res) {
      return res.json().then(function (data) {
        if (!res.ok) throw new Error(data.error || ("HTTP " + res.status));
        return data;
      });
    });
  }

  function status(text, kind) {
    var el = $("status");
    el.textContent = text;
    el.className = kind || "muted";
  }
  function setBusy(b) {
    state.busy = b;
    ["btnFetch", "btnRun"].forEach(function (id) { $(id).disabled = b; });
    $("btnCancel").classList.toggle("hidden", !b);
  }
  function startTimer() {
    state.t0 = Date.now();
    $("timer").textContent = "0.0s";
    state.timer = setInterval(function () {
      $("timer").textContent = ((Date.now() - state.t0) / 1000).toFixed(1) + "s";
    }, 100);
  }
  function stopTimer() {
    if (state.timer) clearInterval(state.timer);
    state.timer = null;
  }
  function log(lines) {
    var el = $("log");
    if (lines && lines.length) {
      el.textContent = lines.join("\n");
      el.scrollTop = el.scrollHeight;
    }
  }
  function appendLog(line) {
    var el = $("log");
    el.textContent += (el.textContent ? "\n" : "") + line;
    el.scrollTop = el.scrollHeight;
  }
  function fail(err, logs) {
    status("出错：" + (err && err.message ? err.message : err), "err");
    appendLog("出错：" + (err && err.message ? err.message : err));
    if (logs) log(logs);
  }

  function fillState(data) {
    state.config = data.config;
    state.defaultPrompt = data.prompt || "";
    $("configPath").textContent = "配置：" + data.configPath;
    $("outDir").value = data.config.outputDir || "";
    $("openWith").value = data.config.openWith || "typora";
    $("summarizer").value = data.config.summarizer === "opencode" ? "opencode" : "api";
    $("model").value = modelForBackend($("summarizer").value);
    $("keepSub").checked = !!data.config.keepSubtitle;
    $("includeTranscript").checked = !!data.config.includeTranscript;
    if (!state.lastText) $("prompt").value = state.defaultPrompt;
    renderHistory(data.history || []);
  }
  function modelForBackend(backend) {
    if (!state.config) return "";
    return backend === "opencode" ? (state.config.opencodeModel || "") : (state.config.model || "");
  }
  function renderHistory(items) {
    var ul = $("history");
    ul.innerHTML = "";
    if (!items.length) {
      var li = document.createElement("li");
      li.className = "muted";
      li.textContent = "还没有记录";
      ul.appendChild(li);
      return;
    }
    items.forEach(function (it) {
      var li = document.createElement("li");
      var t = document.createElement("span");
      t.className = "title";
      t.textContent = it.title || it.input || it.path;
      var b = document.createElement("span");
      b.className = "backend";
      b.textContent = it.backend || "";
      var d = document.createElement("span");
      d.className = "time";
      d.textContent = it.time || "";
      var btn = document.createElement("button");
      btn.textContent = "打开";
      btn.onclick = function () { openNote(it.path, "typora"); };
      li.appendChild(t); li.appendChild(b); li.appendChild(d); li.appendChild(btn);
      ul.appendChild(li);
    });
  }

  function doFetch() {
    var input = $("input").value.trim();
    if (!input) { status("先填 BV 号或链接", "err"); return; }
    setBusy(true);
    status("取字幕中…", "busy");
    startTimer();
    $("resultBox").classList.add("hidden");
    state.ctrl = new AbortController();
    api("/api/fetch", { input: input, engine: $("engine").value }, state.ctrl.signal).then(function (data) {
      if (!data.ok) { fail(new Error(data.error), data.logs); return; }
      log(data.logs || []);
      var v = data.video || {};
      $("subMeta").textContent = v.title + " ｜ " + v.owner + " ｜ " + v.duration + " ｜ " + data.lang + " ｜ " + data.cueCount + " 条 / " + data.chars + " 字";
      $("subText").textContent = data.text || "";
      state.lastText = data.text || "";
      state.lastSrt = data.srt || "";
      status("字幕就绪，可以点「生成稿件」", "ok");
    }).catch(function (err) {
      if (err && err.name === "AbortError") { status("已取消", "err"); return; }
      fail(err);
    }).finally(function () { stopTimer(); setBusy(false); });
  }

  function doRun() {
    var input = $("input").value.trim();
    if (!input) { status("先填 BV 号或链接", "err"); return; }
    setBusy(true);
    status("生成中（取字幕→总结→写文件，约 30–90 秒）…", "busy");
    startTimer();
    $("resultBox").classList.add("hidden");
    state.ctrl = new AbortController();
    var body = {
      input: input,
      prompt: $("prompt").value,
      summarizer: $("summarizer").value,
      model: $("summarizer").value === "api" ? $("model").value.trim() : "",
      opencodeModel: $("summarizer").value === "opencode" ? $("model").value.trim() : "",
      outDir: $("outDir").value.trim(),
      openWith: $("openWith").value,
      keepSubtitle: $("keepSub").checked,
      includeTranscript: $("includeTranscript").checked
    };
    api("/api/summarize", body, state.ctrl.signal).then(function (data) {
      if (!data.ok) { fail(new Error(data.error), data.logs); return; }
      log(data.logs || []);
      state.lastPath = data.path || "";
      $("resultPath").textContent = data.path;
      $("summaryPreview").textContent = data.summary || "";
      $("resultBox").classList.remove("hidden");
      status("已生成：" + (data.chars || 0) + " 字，用时 " + ((data.elapsedMs || 0) / 1000).toFixed(1) + "s", "ok");
      refreshHistory();
    }).catch(function (err) {
      if (err && err.name === "AbortError") { status("已取消", "err"); return; }
      fail(err);
    }).finally(function () { stopTimer(); setBusy(false); });
  }

  function openNote(path, how) {
    if (!path) return;
    api("/api/open", { path: path, how: how }).then(function (data) {
      if (!data.ok) fail(new Error(data.error));
    }).catch(function (err) { fail(err); });
  }
  function showFolder(path) {
    if (!path) return;
    api("/api/open-dir", { path: path }).catch(function (err) { fail(err); });
  }
  function refreshHistory() {
    api("/api/history").then(function (data) {
      if (data.ok) renderHistory(data.history || []);
    }).catch(function () {});
  }

  function download(name, text) {
    var blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    var a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a);
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 500);
  }
  function copyText(text) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(function () { status("已复制", "ok"); }).catch(function () { fallbackCopy(text); });
    } else {
      fallbackCopy(text);
    }
  }
  function fallbackCopy(text) {
    var ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); status("已复制", "ok"); } catch (e) { status("复制失败", "err"); }
    ta.remove();
  }

  function saveConfig() {
    api("/api/config", {
      outputDir: $("outDir").value.trim(),
      summarizer: $("summarizer").value,
      model: state.config ? state.config.model : "",
      opencodeModel: state.config ? state.config.opencodeModel : "",
      openWith: $("openWith").value,
      keepSubtitle: $("keepSub").checked,
      includeTranscript: $("includeTranscript").checked
    }).then(function (data) {
      if (data.ok) {
        state.config = data.config;
        state.config.model = $("summarizer").value === "api" ? $("model").value.trim() : state.config.model;
        state.config.opencodeModel = $("summarizer").value === "opencode" ? $("model").value.trim() : state.config.opencodeModel;
        status("设置已保存", "ok");
      } else {
        fail(new Error(data.error));
      }
    }).catch(function (err) { fail(err); });
  }

  function bind() {
    $("btnFetch").onclick = doFetch;
    $("btnRun").onclick = doRun;
    $("btnCancel").onclick = function () {
      if (state.ctrl) state.ctrl.abort();
    };
    $("btnCopyText").onclick = function () { copyText(state.lastText || ""); };
    $("btnDownloadTxt").onclick = function () { download(baseName() + ".txt", state.lastText || ""); };
    $("btnDownloadSrt").onclick = function () { download(baseName() + ".srt", state.lastSrt || ""); };
    $("btnSavePrompt").onclick = function () {
      api("/api/prompt", { text: $("prompt").value }).then(function (data) {
        if (data.ok) { state.defaultPrompt = $("prompt").value; status("提示词已保存", "ok"); }
        else fail(new Error(data.error));
      }).catch(function (err) { fail(err); });
    };
    $("btnResetPrompt").onclick = function () { $("prompt").value = state.defaultPrompt || ""; status("已恢复默认提示词", "ok"); };
    $("btnOpenTypora").onclick = function () { openNote(state.lastPath, "typora"); };
    $("btnOpenObsidian").onclick = function () { openNote(state.lastPath, "obsidian"); };
    $("btnShowFolder").onclick = function () { showFolder(state.lastPath); };
    $("summarizer").onchange = function () { $("model").value = modelForBackend($("summarizer").value); };
    $("outDir").onchange = saveConfig;
    $("openWith").onchange = saveConfig;
    $("keepSub").onchange = saveConfig;
    $("includeTranscript").onchange = saveConfig;
    $("input").addEventListener("keydown", function (e) { if (e.key === "Enter") doRun(); });
  }

  function baseName() {
    var v = $("subMeta").textContent || "subtitle";
    return v.replace(/[\\/:*?"<>|]/g, "_").slice(0, 80) || "subtitle";
  }

  bind();
  api("/api/state").then(fillState).catch(function (err) {
    status("加载配置失败：" + err.message + "（token 可能不对，重新从终端打开的地址进入）", "err");
  });
})();

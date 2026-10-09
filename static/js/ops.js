// FR-13 容器启停/重启/重建操作：二次确认 + loading + 结果日志展示 + 成功后刷新
(function () {
  var STORE_KEY = 'smsOpResult';

  // setResult 将执行日志渲染到 #op-result 区域；persist=true 时写入 sessionStorage，
  // 以便页面自动刷新后仍能展示 redeploy 的执行日志。
  function setResult(text, kind, persist) {
    if (persist) {
      try { sessionStorage.setItem(STORE_KEY, JSON.stringify({ t: text, k: kind || '' })); } catch (e) {}
    }
    var el = document.getElementById('op-result');
    if (!el) {
      if (kind === 'error' || kind === 'running') { alert(text); }
      return;
    }
    el.hidden = false;
    el.textContent = text || '';
    el.className = 'op-result ' + (kind || '');
    el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
  }

  window.smsOp = function (id, action) {
    var labels = { start: '启动', stop: '停止', restart: '重启', redeploy: '重建' };
    var act = labels[action] || action;
    var msg = action === 'redeploy'
      ? '确定对容器 ' + id + ' 执行【重建/重新部署】？\n流程：停止 → 删除容器 → 删除该服务镜像（被共用则跳过）→ 拉取新镜像 → 启动。\n该操作可能耗时较长，并依赖 docker compose。'
      : '确定对容器 ' + id + ' 执行【' + act + '】操作？';
    if (!confirm(msg)) {
      return;
    }
    var btns = document.querySelectorAll('button[data-id="' + id + '"][data-action="' + action + '"]');
    btns.forEach(function (b) { b.disabled = true; b.textContent = '处理中…'; });

    if (action === 'redeploy') {
      setResult('正在重建容器 ' + id + ' …（停止→删容器→删镜像→拉新镜像→启动），请稍候', 'running', false);
    }

    fetch('/monitor/api/docker/' + encodeURIComponent(id) + '/' + action, { method: 'POST' })
      .then(function (r) { return r.json(); })
      .then(function (j) {
        if (j && j.code === 200) {
          if (action === 'redeploy') {
            var log = (j.data && j.data.log) ? j.data.log : ('重建成功：' + id);
            setResult(log, 'success', true);
            // 刷新以更新容器状态；日志已持久化，刷新后仍会展示
            setTimeout(function () { location.reload(); }, 1500);
          } else {
            alert(act + ' 成功');
            setTimeout(function () { location.reload(); }, 600);
          }
        } else {
          var failMsg = '操作失败：' + (j && j.message ? j.message : '未知错误');
          if (action === 'redeploy') {
            setResult(failMsg, 'error', true);
            setTimeout(function () { location.reload(); }, 2000);
          } else {
            alert(failMsg);
            btns.forEach(function (b) { b.disabled = false; b.textContent = act; });
          }
        }
      })
      .catch(function (e) {
        var errMsg = '请求失败：' + e;
        if (action === 'redeploy') {
          setResult(errMsg, 'error', true);
          setTimeout(function () { location.reload(); }, 2000);
        } else {
          alert(errMsg);
          btns.forEach(function (b) { b.disabled = false; b.textContent = act; });
        }
      });
  };

  // 页面加载后回放上一次 redeploy 的执行日志（跨自动刷新保留）。
  document.addEventListener('DOMContentLoaded', function () {
    try {
      var raw = sessionStorage.getItem(STORE_KEY);
      if (!raw) { return; }
      sessionStorage.removeItem(STORE_KEY);
      var o = JSON.parse(raw);
      if (o && o.t) { setResult(o.t, o.k, false); }
    } catch (e) {}
  });
})();

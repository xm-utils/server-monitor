// Admin 前端脚本

// 自动刷新服务器列表
(function() {
    const refreshInterval = 10000; // 10 秒
    
    function refreshServerList() {
        fetch('/api/servers')
            .then(r => r.json())
            .then(data => {
                if (data.code === 200 && data.data) {
                    updateServerTable(data.data);
                }
            })
            .catch(err => {
                console.warn('刷新服务器列表失败:', err);
            });
    }
    
    function updateServerTable(servers) {
        const tbody = document.querySelector('.server-table tbody');
        if (!tbody) return;
        
        tbody.innerHTML = servers.map(server => `
            <tr class="${server.status}">
                <td><code>${server.id}</code></td>
                <td>${server.hostname}</td>
                <td>${server.ip}</td>
                <td>
                    <span class="status ${server.status}">${server.status}</span>
                </td>
                <td>${server.os}/${server.arch}</td>
                <td>${new Date(server.lastSeen).toLocaleString('zh-CN')}</td>
                <td>
                    <a href="/servers/${server.id}" class="btn">详情</a>
                </td>
            </tr>
        `).join('');
    }
    
    // 如果在列表页，启动自动刷新
    if (window.location.pathname === '/') {
        setInterval(refreshServerList, refreshInterval);
    }
})();

// 删除服务器
function deleteServer(id) {
    if (!confirm('确定要移除该服务器吗？')) return;
    
    fetch(`/api/servers/${id}`, {
        method: 'DELETE'
    })
    .then(r => r.json())
    .then(data => {
        if (data.code === 200) {
            alert('已移除');
            window.location.reload();
        } else {
            alert('移除失败: ' + data.message);
        }
    })
    .catch(err => {
        alert('移除失败: ' + err);
    });
}

// 格式化时间戳
function formatTimestamp(ts) {
    if (!ts) return '-';
    return new Date(ts * 1000).toLocaleString('zh-CN');
}

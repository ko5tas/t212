(function () {
  'use strict';

  const statusEl = document.getElementById('status');
  const updatedEl = document.getElementById('updated');
  const tableEl = document.getElementById('positions');
  const tbodyEl = document.getElementById('tbody');
  const emptyEl = document.getElementById('empty');

  function fmt(n) { return '£' + n.toFixed(2); }

  function render(msg) {
    const positions = msg.positions || [];
    tbodyEl.innerHTML = '';

    if (positions.length === 0) {
      tableEl.classList.add('hidden');
      emptyEl.classList.remove('hidden');
    } else {
      emptyEl.classList.add('hidden');
      tableEl.classList.remove('hidden');
      positions.forEach(function (p) {
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + p.ticker + '</td>' +
          '<td>' + p.quantity + '</td>' +
          '<td>' + fmt(p.averagePrice) + '</td>' +
          '<td>' + fmt(p.currentPrice) + '</td>' +
          '<td class="profit">+' + fmt(p.profitPerShare) + '</td>' +
          '<td>' + fmt(p.marketValue) + '</td>';
        tbodyEl.appendChild(tr);
      });
    }

    const ts = new Date(msg.timestamp);
    updatedEl.textContent = 'Last updated: ' + ts.toLocaleTimeString();
  }

  function connect() {
    const ws = new WebSocket('ws://' + location.host + '/ws');

    ws.onopen = function () {
      statusEl.textContent = 'Live';
      statusEl.className = 'status connected';
    };

    ws.onmessage = function (e) {
      try { render(JSON.parse(e.data)); } catch (_) {}
    };

    ws.onclose = function () {
      statusEl.textContent = 'Reconnecting…';
      statusEl.className = 'status disconnected';
      setTimeout(connect, 3000);
    };
  }

  connect();
})();

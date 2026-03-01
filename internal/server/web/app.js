(function () {
  'use strict';

  var statusEl = document.getElementById('status');
  var updatedEl = document.getElementById('updated');
  var tableEl = document.getElementById('positions');
  var tbodyEl = document.getElementById('tbody');
  var emptyEl = document.getElementById('empty');

  var symbols = { GBP: '£', USD: '$', EUR: '€' };
  function sym(currency) { return symbols[currency] || currency + ' '; }
  function fmt(n, currency) { return sym(currency) + n.toFixed(2); }

  var ws = null;

  function sendRefresh(ticker) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ action: 'refresh', ticker: ticker }));
    }
  }

  function sendRefreshAll() {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ action: 'refresh_all' }));
    }
  }

  document.getElementById('refresh-all').addEventListener('click', sendRefreshAll);

  function render(msg) {
    var positions = msg.positions || [];
    tbodyEl.innerHTML = '';

    if (positions.length === 0) {
      tableEl.classList.add('hidden');
      emptyEl.classList.remove('hidden');
    } else {
      emptyEl.classList.add('hidden');
      tableEl.classList.remove('hidden');
      positions.forEach(function (p) {
        var c = p.currency || 'GBP';
        var r = p.returns;
        var retVal = r ? fmt(r['return'], 'GBP') : '--';
        var retPct = r ? r.returnPct.toFixed(1) + '%' : '--';
        var netRoi = r ? r.netRoiPct.toFixed(1) + '%' : '--';
        var tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + p.ticker + '</td>' +
          '<td><button class="btn-refresh-row" title="Refresh ' + p.ticker + '">&#x21bb;</button></td>' +
          '<td>' + retVal + '</td>' +
          '<td>' + retPct + '</td>' +
          '<td>' + netRoi + '</td>' +
          '<td>' + p.quantity + '</td>' +
          '<td>' + fmt(p.averagePrice, c) + '</td>' +
          '<td>' + fmt(p.currentPrice, c) + '</td>' +
          '<td class="profit">+' + fmt(p.profitPerShare, c) + '</td>' +
          '<td>' + fmt(p.marketValue, c) + '</td>';
        tr.querySelector('.btn-refresh-row').addEventListener('click', function () {
          sendRefresh(p.ticker);
        });
        tbodyEl.appendChild(tr);
      });
    }

    var ts = new Date(msg.timestamp);
    updatedEl.textContent = 'Last updated: ' + ts.toLocaleTimeString();
  }

  function connect() {
    ws = new WebSocket('ws://' + location.host + '/ws');

    ws.onopen = function () {
      statusEl.textContent = 'Live';
      statusEl.className = 'status connected';
    };

    ws.onmessage = function (e) {
      try { render(JSON.parse(e.data)); } catch (_) {}
    };

    ws.onclose = function () {
      ws = null;
      statusEl.textContent = 'Reconnecting…';
      statusEl.className = 'status disconnected';
      setTimeout(connect, 3000);
    };
  }

  connect();
})();

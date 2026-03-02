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
  var lastPositions = [];
  var sortCol = 'profitPerShare';
  var sortAsc = false;

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

  function colValue(p, col) {
    switch (col) {
      case 'ticker': return p.ticker || '';
      case 'name': return p.name || '';
      case 'return': return p.returns ? p.returns['return'] : 0;
      case 'returnPct': return p.returns ? p.returns.returnPct : 0;
      case 'netRoi': return p.returns ? p.returns.netRoiPct : 0;
      case 'quantity': return p.quantity || 0;
      case 'currentPrice': return p.currentPrice || 0;
      case 'averagePrice': return p.averagePrice || 0;
      case 'profitPerShare': return p.profitPerShare || 0;
      case 'marketValue': return p.marketValue || 0;
      default: return 0;
    }
  }

  function sortPositions(positions) {
    var sorted = positions.slice();
    sorted.sort(function (a, b) {
      var va = colValue(a, sortCol);
      var vb = colValue(b, sortCol);
      var cmp;
      if (typeof va === 'string') {
        cmp = va.localeCompare(vb);
      } else {
        cmp = va - vb;
      }
      return sortAsc ? cmp : -cmp;
    });
    return sorted;
  }

  function updateSortIndicators() {
    var ths = document.querySelectorAll('th[data-col]');
    ths.forEach(function (th) {
      var base = th.textContent.replace(/ [▲▼]$/, '');
      if (th.getAttribute('data-col') === sortCol) {
        th.textContent = base + (sortAsc ? ' ▲' : ' ▼');
      } else {
        th.textContent = base;
      }
    });
  }

  document.querySelectorAll('th[data-col]').forEach(function (th) {
    th.style.cursor = 'pointer';
    th.addEventListener('click', function () {
      var col = th.getAttribute('data-col');
      if (sortCol === col) {
        sortAsc = !sortAsc;
      } else {
        sortCol = col;
        sortAsc = false;
      }
      updateSortIndicators();
      renderPositions(lastPositions);
    });
  });

  function renderPositions(positions) {
    tbodyEl.innerHTML = '';
    var sorted = sortPositions(positions);

    if (sorted.length === 0) {
      tableEl.classList.add('hidden');
      emptyEl.classList.remove('hidden');
    } else {
      emptyEl.classList.add('hidden');
      tableEl.classList.remove('hidden');
      sorted.forEach(function (p) {
        var c = p.currency || 'GBP';
        var r = p.returns;
        var retVal = r ? fmt(r['return'], 'GBP') : '--';
        var retPct = r ? r.returnPct.toFixed(1) + '%' : '--';
        var netRoi = r ? r.netRoiPct.toFixed(1) + '%' : '--';
        var ppsClass = p.profitPerShare >= 0 ? 'profit' : 'loss';
        var ppsSign = p.profitPerShare >= 0 ? '+' : '';
        var tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + p.ticker + '</td>' +
          '<td>' + (p.name || '') + '</td>' +
          '<td><button class="btn-refresh-row" title="Refresh ' + p.ticker + '">&#x21bb;</button></td>' +
          '<td>' + retVal + '</td>' +
          '<td>' + retPct + '</td>' +
          '<td>' + netRoi + '</td>' +
          '<td>' + p.quantity + '</td>' +
          '<td>' + fmt(p.currentPrice, c) + '</td>' +
          '<td>' + fmt(p.averagePrice, c) + '</td>' +
          '<td class="' + ppsClass + '">' + ppsSign + fmt(p.profitPerShare, c) + '</td>' +
          '<td>' + fmt(p.marketValue, c) + '</td>';
        tr.querySelector('.btn-refresh-row').addEventListener('click', function () {
          sendRefresh(p.ticker);
        });
        tbodyEl.appendChild(tr);
      });
    }
  }

  function render(msg) {
    lastPositions = msg.positions || [];
    renderPositions(lastPositions);

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

  updateSortIndicators();
  connect();
})();

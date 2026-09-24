(function () {
  var drawer = document.getElementById('drawer');
  var scrim = document.querySelector('.scrim');
  var lastFocused = null;

  function focusable() {
    if (!drawer) return [];
    return Array.prototype.slice.call(
      drawer.querySelectorAll('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ).filter(function (el) { return !el.disabled && el.offsetParent !== null; });
  }

  function openDrawer() {
    if (!drawer) return;
    lastFocused = document.activeElement;
    drawer.classList.add('open');
    drawer.setAttribute('aria-hidden', 'false');
    scrim.classList.add('open');
    document.body.classList.add('no-scroll');
    var f = focusable()[0];
    if (f) f.focus();
  }
  function closeDrawer() {
    if (!drawer) return;
    drawer.classList.remove('open');
    drawer.setAttribute('aria-hidden', 'true');
    scrim.classList.remove('open');
    document.body.classList.remove('no-scroll');
    if (lastFocused) { lastFocused.focus(); lastFocused = null; }
  }

  document.addEventListener('click', function (e) {
    if (e.target.closest('[data-close-cart]')) { closeDrawer(); return; }

    var thumb = e.target.closest('[data-thumb]');
    if (thumb) {
      var main = document.getElementById('gallery-main');
      main.src = thumb.dataset.src;
      main.srcset = thumb.dataset.srcset;
      main.alt = thumb.dataset.alt || '';
      document.querySelectorAll('[data-thumb]').forEach(function (t) { t.classList.toggle('on', t === thumb); });
      return;
    }

    var step = e.target.closest('[data-step]');
    if (step) {
      var inp = step.parentElement.querySelector('input[type=number]');
      var v = (parseInt(inp.value, 10) || 1) + parseInt(step.dataset.step, 10);
      var lo = parseInt(inp.min, 10) || 0, hi = parseInt(inp.max, 10) || 99;
      inp.value = Math.min(Math.max(v, lo), hi);
      inp.dispatchEvent(new Event('change', { bubbles: true }));
    }
  });

  document.addEventListener('change', function (e) {
    var pill = e.target.closest('input[name=variant_id]');
    if (!pill) return;
    var price = document.getElementById('price');
    var stock = document.getElementById('stock');
    var qty = document.querySelector('input[name=qty]');
    if (price) price.textContent = pill.dataset.priceText;
    if (stock) stock.textContent = pill.dataset.stockText;
    if (qty) { qty.max = pill.dataset.stock; if (+qty.value > +pill.dataset.stock) qty.value = pill.dataset.stock; }
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { closeDrawer(); return; }
    if (e.key !== 'Tab' || !drawer || !drawer.classList.contains('open')) return;
    var els = focusable();
    if (!els.length) return;
    var first = els[0], last = els[els.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault(); last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault(); first.focus();
    }
  });

  document.body.addEventListener('htmx:afterSwap', function (e) {
    if (e.detail.target && e.detail.target.id === 'drawer') openDrawer();
  });
})();

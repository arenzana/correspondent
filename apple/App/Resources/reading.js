// Reading aids for the article page, in the app's own script world (the
// article's scripts never run): bionic reading and the spotlight. The web's
// ui/src/lib/reading.ts, for a page that scrolls as a whole. The page says
// what to do in <body data-bionic data-spotlight data-line>.
(function () {
  var body = document.body;
  var prose = document.querySelector('.prose');
  if (!body || !prose) return;
  // Smooth, unless the system is set to reduce motion.
  var motion = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth';

  // Bionic reading: about the first 40% of each word in bold, at least one letter.
  function boldLength(word) {
    var n = Array.from(word).length;
    if (n <= 3) return 1;
    if (n === 4) return 2;
    return Math.ceil(n * 0.4);
  }
  var SKIP = { CODE: 1, PRE: 1, KBD: 1, SAMP: 1, SCRIPT: 1, STYLE: 1, B: 1, STRONG: 1 };
  if (body.dataset.bionic === '1') {
    var texts = [];
    var walker = document.createTreeWalker(prose, NodeFilter.SHOW_TEXT, {
      acceptNode: function (n) {
        for (var p = n.parentElement; p && p !== prose; p = p.parentElement) if (SKIP[p.tagName]) return NodeFilter.FILTER_REJECT;
        return /\p{L}/u.test(n.nodeValue || '') ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_REJECT;
      },
    });
    while (walker.nextNode()) texts.push(walker.currentNode);
    texts.forEach(function (t) {
      var frag = document.createDocumentFragment();
      (t.nodeValue || '').split(/(\p{L}[\p{L}\p{M}'’-]*)/u).forEach(function (part) {
        if (!part) return;
        if (/^\p{L}/u.test(part)) {
          var chars = Array.from(part);
          var k = boldLength(part);
          var b = document.createElement('b');
          b.className = 'bionic';
          b.textContent = chars.slice(0, k).join('');
          frag.append(b, chars.slice(k).join(''));
        } else {
          frag.append(part);
        }
      });
      t.replaceWith(frag);
    });
  }

  // The spotlight. The reading line sits a little above the middle.
  var mode = body.dataset.spotlight;
  var lineH = parseFloat(body.dataset.line) || 30;
  var READING_LINE = 0.4;
  var step = function () {};

  if (mode === 'paragraph') {
    var SEL = 'p, li, blockquote, pre, figure, h1, h2, h3, h4, h5, h6, table, dd, dt';
    var blocks = Array.from(prose.querySelectorAll(SEL)).filter(function (el) { return !el.querySelector(SEL); });
    blocks.forEach(function (b) { b.classList.add('sl'); });
    body.classList.add('spotlit');
    var lit = -1;
    var light = function (i) {
      if (i === lit || i < 0 || i >= blocks.length) return;
      if (blocks[lit]) blocks[lit].classList.remove('lit');
      blocks[i].classList.add('lit');
      lit = i;
    };
    var nearest = function () {
      var line = window.innerHeight * READING_LINE;
      var best = 0, bestD = Infinity;
      blocks.forEach(function (b, i) {
        var r = b.getBoundingClientRect();
        var d = line < r.top ? r.top - line : line > r.bottom ? line - r.bottom : 0;
        if (d < bestD) { best = i; bestD = d; }
      });
      return best;
    };
    var stepping = false, frame = 0;
    window.addEventListener('scroll', function () {
      if (stepping) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(function () { light(nearest()); });
    }, { passive: true });
    step = function (by) {
      var i = Math.min(Math.max(lit + by, 0), blocks.length - 1);
      light(i);
      var r = blocks[i].getBoundingClientRect();
      stepping = true;
      window.scrollBy({ top: r.top + r.height / 2 - window.innerHeight * READING_LINE, behavior: motion });
      setTimeout(function () { stepping = false; }, 450);
    };
    light(nearest());
  } else if (mode === 'line') {
    // A band one line tall stays clear; above and below are veiled.
    var above = document.createElement('div');
    var below = document.createElement('div');
    above.className = 'veil';
    below.className = 'veil';
    body.append(above, below);
    var place = function () {
      var top = window.innerHeight * READING_LINE - lineH * 0.65;
      above.style.cssText = 'top:0;height:' + Math.max(0, top) + 'px';
      below.style.cssText = 'top:' + (top + lineH * 1.3) + 'px;bottom:0';
    };
    place();
    window.addEventListener('resize', place);
    step = function (by) { window.scrollBy({ top: by * lineH, behavior: motion }); };
  }

  // The app sends ↓ and ↑ on the Mac; a keyboard on iPad lands here.
  window.correspondentSpotlight = function (by) { step(by); };
  if (mode === 'paragraph' || mode === 'line') {
    document.addEventListener('keydown', function (e) {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === 'ArrowDown') { step(1); e.preventDefault(); }
      if (e.key === 'ArrowUp') { step(-1); e.preventDefault(); }
    });
  }
})();

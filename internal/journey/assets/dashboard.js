
(function () {
  if (!('IntersectionObserver' in window)) return;
  var links = {};
  document.querySelectorAll('nav.rail a[href^="#"]').forEach(function (a) {
    links[a.getAttribute('href').slice(1)] = a;
  });
  var obs = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      var link = links[e.target.id];
      if (link) link.classList.toggle('active', e.isIntersecting);
    });
  }, { rootMargin: '-8% 0px -82% 0px' });
  document.querySelectorAll('section.block, .task[id], .srow[id]').forEach(function (el) { obs.observe(el); });
})();

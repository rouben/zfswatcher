// Fills the enclosure table; the drive data comes from a JSON data block
// so that no inline script is needed (Content-Security-Policy).
(function () {
  var el = document.getElementById("enclosure-drives");
  if (!el) { return; }
  var data;
  try { data = JSON.parse(el.textContent); } catch (e) { return; }
  ["drives1", "drives2"].forEach(function (key, idx) {
    var drives = data[key] || [];
    drives.forEach(function (d, i) {
      var cell = $("#drive" + (idx + 1) + "-" + (i + 1));
      if (!cell.length) { return; }
      var cls = "is" + d.State;
      cell.append($("<span>").addClass(cls).append($("<kbd>").text(d.Name)));
      cell.addClass(cls);
    });
  });
})();

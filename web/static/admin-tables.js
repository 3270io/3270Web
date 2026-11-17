/* Table support for the administration pages.
   -------------------------------------------------------------------------
   On a phone the admin tables are drawn as cards (admin.css), and a card
   needs two things a table does not: each cell has to say which column it is
   in, because the heading row is no longer beside it, and a cell with nothing
   in it has to be left out. The pages build their rows themselves, at every
   refresh and in several places, so this does it once, here, by watching the
   bodies of the tables: whatever a page puts in a row gets its column's name.

   It also gives the table its roles explicitly. They are the ones the
   elements already have, and they cost nothing on a desktop; what they are
   for is the phone, where making the cells blocks stops some screen readers
   (Safari with VoiceOver, notably) from treating the thing as a table at all. */
(function () {
  'use strict';

  var tables = document.querySelectorAll('table.admin-table');
  if (!tables.length) return;

  function headingNames(table) {
    var names = [];
    table.querySelectorAll('thead th').forEach(function (th) {
      // A column whose heading is only for screen readers ("Actions") has no
      // name worth printing above its buttons.
      names.push(th.querySelector('.visually-hidden') ? '' : th.textContent.trim());
    });
    return names;
  }

  // "—" is what these pages write for "none", and a column of dashes under
  // labels is noise on a card.
  function isEmpty(cell) {
    if (cell.querySelector('button, a, input, select')) return false;
    var text = cell.textContent.trim();
    return text === '' || text === '—' || text === '–';
  }

  function label(table, names) {
    table.querySelectorAll('tbody tr').forEach(function (row) {
      row.setAttribute('role', 'row');
      // The loading placeholder is shapes, not data: nothing to name or hide.
      var placeholder = row.classList.contains('admin-skeleton');
      Array.prototype.forEach.call(row.children, function (cell, i) {
        cell.setAttribute('role', 'cell');
        // A cell spanning the row ("No accounts yet") belongs to no column.
        if (cell.colSpan > 1 || placeholder) return;
        if (names[i]) {
          cell.setAttribute('data-label', names[i]);
        } else {
          cell.removeAttribute('data-label');
        }
        if (isEmpty(cell)) {
          cell.setAttribute('data-empty', '');
        } else {
          cell.removeAttribute('data-empty');
        }
      });
    });
  }

  tables.forEach(function (table) {
    table.setAttribute('role', 'table');
    table.querySelectorAll('thead, tbody').forEach(function (group) {
      group.setAttribute('role', 'rowgroup');
    });
    table.querySelectorAll('thead tr').forEach(function (row) {
      row.setAttribute('role', 'row');
    });
    table.querySelectorAll('thead th').forEach(function (th) {
      th.setAttribute('role', 'columnheader');
    });

    var names = headingNames(table);
    label(table, names);

    // Only the child list is watched. The attributes set above are not, so
    // the labelling cannot trigger itself.
    var observer = new MutationObserver(function () { label(table, names); });
    table.querySelectorAll('tbody').forEach(function (body) {
      observer.observe(body, { childList: true });
    });
  });
})();

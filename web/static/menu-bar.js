// The menu bar — one strip of chrome across the top of a session, and the
// drop-downs hanging off it.
//
// What this file is NOT: a menu framework that owns the commands. Every item
// in every panel is the same element it was when it lived on a toolbar row —
// the same button with the same data-attribute, the same <form> posting to the
// same route. ui.js, workflow.js, hotspots.js, terminal-controls.js and the
// command palette all still find their controls with the selectors they always
// used. This file only decides which panel is open and where it sits.
//
// That is deliberate. The controls a 3270 operator needs are spread across a
// dozen modules written at different times; a menu that re-declared them would
// be a second copy of the truth, and the copy would rot.
(function () {
  "use strict";

  var OPEN_CLASS = "is-open";

  // Every menu on the page, discovered once. A menu is a wrapper carrying
  // data-app-menu with a trigger and a panel inside it.
  var menus = [];
  var openMenu = null;
  // Whether the currently open menu was opened from the keyboard. A menu
  // opened with the mouse should not steal focus from wherever the operator
  // was; one opened with Enter or ArrowDown must, or there is nothing to
  // arrow through.
  var openedByKeyboard = false;

  function isVisible(el) {
    if (!el || el.hidden || el.disabled) {
      return false;
    }
    for (var node = el; node && node !== document.body; node = node.parentElement) {
      if (node.hasAttribute && node.hasAttribute("hidden")) {
        return false;
      }
    }
    return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
  }

  function itemsOf(menu) {
    var nodes = menu.panel.querySelectorAll(
      '[role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"]'
    );
    var out = [];
    for (var i = 0; i < nodes.length; i++) {
      if (isVisible(nodes[i])) {
        out.push(nodes[i]);
      }
    }
    return out;
  }

  // A fixed panel is positioned against the layout viewport, so that is what it
  // has to be clamped to. window.innerWidth and .innerHeight report the visual
  // viewport, which on a phone is a different box the moment anything overflows
  // or the page is pinch-zoomed — clamping to it puts the panel's right-hand
  // edge somewhere off the side of the screen.
  function viewportWidth() {
    var width = document.documentElement ? document.documentElement.clientWidth : 0;
    return width > 0 ? width : window.innerWidth;
  }

  function viewportHeight() {
    var height = document.documentElement ? document.documentElement.clientHeight : 0;
    return height > 0 ? height : window.innerHeight;
  }

  // Panels are positioned in JS rather than by CSS alone because they are
  // fixed-position (so no ancestor's overflow can clip them) and must stay
  // inside the viewport on a narrow screen, where a right-hand menu would
  // otherwise hang off the edge with its items unreachable.
  function position(menu) {
    var panel = menu.panel;
    var rect = menu.trigger.getBoundingClientRect();
    var margin = 8;

    panel.style.maxHeight = Math.max(180, viewportHeight() - rect.bottom - margin * 2) + "px";
    panel.style.left = "0px";
    panel.style.top = rect.bottom + 4 + "px";

    var width = panel.offsetWidth;
    var left = menu.alignEnd ? rect.right - width : rect.left;
    var maxLeft = viewportWidth() - width - margin;
    if (left > maxLeft) {
      left = maxLeft;
    }
    if (left < margin) {
      left = margin;
    }
    panel.style.left = Math.round(left) + "px";
  }

  function close(menu, restoreFocus) {
    if (!menu || !menu.root.classList.contains(OPEN_CLASS)) {
      return;
    }
    menu.root.classList.remove(OPEN_CLASS);
    menu.panel.hidden = true;
    menu.trigger.setAttribute("aria-expanded", "false");
    if (openMenu === menu) {
      openMenu = null;
    }
    if (restoreFocus) {
      menu.trigger.focus();
    }
  }

  function closeAll(restoreFocus) {
    for (var i = 0; i < menus.length; i++) {
      close(menus[i], restoreFocus && menus[i] === openMenu);
    }
  }

  function open(menu, fromKeyboard) {
    if (openMenu === menu) {
      return;
    }
    closeAll(false);
    menu.root.classList.add(OPEN_CLASS);
    menu.panel.hidden = false;
    menu.trigger.setAttribute("aria-expanded", "true");
    openMenu = menu;
    openedByKeyboard = !!fromKeyboard;
    if (typeof menu.onOpen === "function") {
      menu.onOpen();
    }
    position(menu);
    if (fromKeyboard) {
      var items = itemsOf(menu);
      if (items.length) {
        items[0].focus();
      }
    }
  }

  function toggle(menu, fromKeyboard) {
    if (openMenu === menu) {
      close(menu, true);
    } else {
      open(menu, fromKeyboard);
    }
  }

  function menuIndex(menu) {
    for (var i = 0; i < menus.length; i++) {
      if (menus[i] === menu) {
        return i;
      }
    }
    return -1;
  }

  // Left and right move along the bar, the way a desktop menu bar does, and
  // skip whichever menus are hidden — Automation is not there in Business
  // mode, and arrowing into an invisible panel is the kind of dead end that
  // makes people stop using the keyboard.
  function siblingMenu(menu, delta) {
    var index = menuIndex(menu);
    if (index === -1) {
      return null;
    }
    for (var step = 1; step <= menus.length; step++) {
      var next = menus[(index + delta * step + menus.length * step) % menus.length];
      if (isVisible(next.trigger)) {
        return next;
      }
    }
    return null;
  }

  function focusItem(menu, delta, absolute) {
    var items = itemsOf(menu);
    if (!items.length) {
      return;
    }
    var current = items.indexOf(document.activeElement);
    var next;
    if (absolute === "first") {
      next = 0;
    } else if (absolute === "last") {
      next = items.length - 1;
    } else if (current === -1) {
      next = delta > 0 ? 0 : items.length - 1;
    } else {
      next = (current + delta + items.length) % items.length;
    }
    items[next].focus();
  }

  // Typing a letter jumps to the next item starting with it, which is how
  // every desktop menu has behaved for thirty years.
  function typeahead(menu, char) {
    var items = itemsOf(menu);
    if (!items.length) {
      return false;
    }
    var start = items.indexOf(document.activeElement) + 1;
    for (var i = 0; i < items.length; i++) {
      var item = items[(start + i) % items.length];
      var label = item.querySelector(".appmenu-label");
      var text = (label ? label.childNodes[0] : item).textContent || "";
      if (text.trim().toLowerCase().charAt(0) === char) {
        item.focus();
        return true;
      }
    }
    return false;
  }

  /* ------------------------------------------------------------------ */
  /* Activation                                                          */
  /* ------------------------------------------------------------------ */

  // After an item runs, the menu goes away and the terminal takes the
  // keyboard back — unless the item opened a dialog, which wants the focus
  // itself. Without this, focus would sit on a menu item that is no longer on
  // screen, and the operator's next keystroke would activate it again instead
  // of reaching the host.
  function afterActivate() {
    window.requestAnimationFrame(function () {
      var dialogs = document.querySelectorAll('[role="dialog"]');
      for (var i = 0; i < dialogs.length; i++) {
        var dialog = dialogs[i];
        if (dialog.offsetWidth || dialog.offsetHeight || dialog.getClientRects().length) {
          return;
        }
      }
      var api = window.ThreeSeventyWeb;
      if (api && api.terminal && typeof api.terminal.focusTerminal === "function") {
        api.terminal.focusTerminal();
      }
    });
  }

  /* ------------------------------------------------------------------ */
  /* Checkbox items                                                      */
  /* ------------------------------------------------------------------ */

  // focus-mode.js, hotspots.js, terminal-controls.js and workspace-mode.js all
  // maintain aria-pressed on their toggles, because those toggles used to be
  // toolbar buttons. In a menu the right property is aria-checked. Rather than
  // teach four modules about menus, mirror one onto the other.
  function mirrorPressedState(item) {
    var sync = function () {
      item.setAttribute("aria-checked", item.getAttribute("aria-pressed") === "true" ? "true" : "false");
    };
    sync();
    new MutationObserver(sync).observe(item, {
      attributes: true,
      attributeFilter: ["aria-pressed"]
    });
  }

  /* ------------------------------------------------------------------ */
  /* The theme picker in the View menu                                   */
  /* ------------------------------------------------------------------ */

  // Themes load asynchronously (some are files on disk), so the list is filled
  // each time the menu opens rather than once at startup.
  function initThemeSelect(select) {
    var fill = function () {
      var api = window.ThreeSeventyWeb;
      if (!api || typeof api.listThemes !== "function") {
        return;
      }
      var themes = api.listThemes();
      var current = typeof api.getCurrentTheme === "function" ? api.getCurrentTheme() : "";
      var signature = themes
        .map(function (theme) {
          return theme.id;
        })
        .join("|");
      if (select.dataset.signature !== signature) {
        select.dataset.signature = signature;
        select.innerHTML = "";
        themes.forEach(function (theme) {
          var option = document.createElement("option");
          option.value = theme.id;
          option.textContent = theme.name;
          select.appendChild(option);
        });
      }
      if (current) {
        select.value = current;
      }
    };

    select.addEventListener("change", function () {
      var api = window.ThreeSeventyWeb;
      if (api && typeof api.applyTheme === "function") {
        api.applyTheme(select.value);
      }
    });

    return fill;
  }

  /* ------------------------------------------------------------------ */
  /* Wiring                                                              */
  /* ------------------------------------------------------------------ */

  function initMenu(root) {
    var trigger = root.querySelector("[data-app-menu-trigger]");
    var panel = root.querySelector("[data-app-menu-panel]");
    if (!trigger || !panel) {
      return null;
    }

    var menu = {
      root: root,
      trigger: trigger,
      panel: panel,
      alignEnd: root.classList.contains("appmenu-end")
    };

    var themeSelect = panel.querySelector("[data-app-menu-theme]");
    if (themeSelect) {
      menu.onOpen = initThemeSelect(themeSelect);
    }

    trigger.addEventListener("click", function (event) {
      event.preventDefault();
      // detail is 0 for a click synthesised by Enter or Space on the button,
      // which is how we tell "opened from the keyboard" from "clicked".
      toggle(menu, event.detail === 0);
    });

    // Sliding along an open bar switches menus without a second click. Only
    // while something is already open: hovering the bar on the way past
    // should not open anything.
    trigger.addEventListener("pointerenter", function () {
      if (openMenu && openMenu !== menu) {
        open(menu, false);
      }
    });

    panel.addEventListener("click", function (event) {
      var item = event.target.closest(
        '[role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"]'
      );
      if (!item || item.disabled) {
        return;
      }
      // A submit button has to reach its form before the panel is torn down,
      // and closing on the next frame is early enough that nothing is seen.
      window.requestAnimationFrame(function () {
        close(menu, false);
        afterActivate();
      });
    });

    var checkboxes = panel.querySelectorAll('[role="menuitemcheckbox"]');
    for (var i = 0; i < checkboxes.length; i++) {
      mirrorPressedState(checkboxes[i]);
    }

    return menu;
  }

  function onKeyDown(event) {
    if (event.defaultPrevented) {
      return;
    }

    var target = event.target;
    var inMenu = target && typeof target.closest === "function" ? target.closest("[data-app-menu]") : null;

    // On the bar itself, before anything is open: left and right walk the
    // menus, down opens one.
    if (!openMenu) {
      if (!inMenu) {
        return;
      }
      var barMenu = null;
      for (var i = 0; i < menus.length; i++) {
        if (menus[i].root === inMenu) {
          barMenu = menus[i];
        }
      }
      if (!barMenu || target !== barMenu.trigger) {
        return;
      }
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        open(barMenu, true);
        if (event.key === "ArrowUp") {
          focusItem(barMenu, 0, "last");
        }
        return;
      }
      if (event.key === "ArrowRight" || event.key === "ArrowLeft") {
        var neighbour = siblingMenu(barMenu, event.key === "ArrowRight" ? 1 : -1);
        if (neighbour) {
          event.preventDefault();
          neighbour.trigger.focus();
        }
        return;
      }
      // Escape on the bar with nothing open hands the keyboard back to the
      // terminal. Closing a menu leaves focus on its trigger, which is the
      // correct place for it to be; a second Escape is how you say "and now
      // let me get back to work".
      if (event.key === "Escape") {
        event.preventDefault();
        var api = window.ThreeSeventyWeb;
        if (api && api.terminal && typeof api.terminal.focusTerminal === "function") {
          api.terminal.focusTerminal();
        }
      }
      return;
    }

    // With a menu open the bar owns the keyboard: keyboard.js's host key
    // handler already stands down (isModalOpen() counts an open menu), and
    // stopping propagation here keeps stray keys away from anything else.
    var menu = openMenu;

    switch (event.key) {
      case "Escape":
        event.preventDefault();
        event.stopPropagation();
        close(menu, true);
        return;
      case "ArrowDown":
        event.preventDefault();
        event.stopPropagation();
        focusItem(menu, 1);
        return;
      case "ArrowUp":
        event.preventDefault();
        event.stopPropagation();
        focusItem(menu, -1);
        return;
      case "Home":
        event.preventDefault();
        event.stopPropagation();
        focusItem(menu, 0, "first");
        return;
      case "End":
        event.preventDefault();
        event.stopPropagation();
        focusItem(menu, 0, "last");
        return;
      case "ArrowRight":
      case "ArrowLeft":
        // Inside a text field or a slider the arrows belong to the control.
        if (target && target.closest("input, select, textarea")) {
          return;
        }
        var next = siblingMenu(menu, event.key === "ArrowRight" ? 1 : -1);
        if (next) {
          event.preventDefault();
          event.stopPropagation();
          open(next, true);
        }
        return;
      case "Tab":
        // Tab leaves the menu entirely rather than walking its items; that is
        // the menu-bar convention, and it is also the only way out for
        // somebody who opened a menu by accident.
        close(menu, false);
        return;
      default:
        break;
    }

    if (
      event.key &&
      event.key.length === 1 &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey &&
      !(target && target.closest("input, select, textarea"))
    ) {
      if (typeahead(menu, event.key.toLowerCase())) {
        event.preventDefault();
        event.stopPropagation();
      }
    }
  }

  function init() {
    var roots = document.querySelectorAll("[data-app-menu]");
    for (var i = 0; i < roots.length; i++) {
      var menu = initMenu(roots[i]);
      if (menu) {
        menus.push(menu);
      }
    }
    if (!menus.length) {
      return;
    }

    // Capture, so a click on a control that stops propagation still dismisses
    // an open menu.
    document.addEventListener(
      "pointerdown",
      function (event) {
        if (!openMenu) {
          return;
        }
        if (event.target && typeof event.target.closest === "function" && event.target.closest("[data-app-menu]")) {
          return;
        }
        closeAll(false);
      },
      true
    );

    document.addEventListener("keydown", onKeyDown, true);

    // A menu pinned to a trigger that has moved is a menu pointing at nothing.
    window.addEventListener(
      "resize",
      function () {
        if (openMenu) {
          position(openMenu);
        }
      },
      { passive: true }
    );
    window.addEventListener(
      "scroll",
      function () {
        if (openMenu) {
          position(openMenu);
        }
      },
      { passive: true, capture: true }
    );

    // Collapsing the bar, entering focus mode or switching workspace mode all
    // move or remove the triggers underneath an open panel.
    document.addEventListener("workspacemodechange", function () {
      closeAll(false);
    });

    window.ThreeSeventyWeb = window.ThreeSeventyWeb || {};
    window.ThreeSeventyWeb.menuBar = {
      close: function () {
        closeAll(false);
      },
      isOpen: function () {
        return !!openMenu;
      },
      openedByKeyboard: function () {
        return openedByKeyboard;
      }
    };
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();

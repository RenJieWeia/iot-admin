// 管理后台交互增强：无 JS 时表单/链接仍可用（服务端渲染 + 整页跳转）。
(function () {
  "use strict";

  const RETURN_KEY = "ui:drawer-return-href";
  const SCROLL_PREFIX = "ui:scroll:";

  function $(sel, root) {
    return (root || document).querySelector(sel);
  }

  /* ---------- 抽屉：Esc 关闭、关闭后焦点回到触发元素 ---------- */
  function closeDrawer() {
    const close = $("#drawer-close");
    if (!close) return;
    const back = sessionStorage.getItem(RETURN_KEY);
    sessionStorage.removeItem(RETURN_KEY);
    if (back) {
      const trigger = document.querySelector('a[href="' + back + '"]');
      if (trigger) {
        trigger.setAttribute("data-focus-return", "1");
      }
    }
    window.location = close.getAttribute("href");
  }

  function rememberTrigger(el) {
    if (!el || !el.getAttribute) return;
    const href = el.getAttribute("href") || "";
    if (href.indexOf("drawer=") >= 0) {
      sessionStorage.setItem(RETURN_KEY, href);
    }
  }

  /* ---------- 删除二次确认 ---------- */
  let deleteTrigger = null;

  function openDeleteDialog(url, name, trigger) {
    const dialog = $("#delete-dialog");
    const form = $("#delete-form");
    if (!dialog || !form) return;
    deleteTrigger = trigger || null;
    form.setAttribute("action", url);
    const text = $("#delete-text");
    if (text) {
      text.textContent = "删除后不可恢复。确定删除 " + (name || "这一行") + " 吗？";
    }
    $("#delete-backdrop").hidden = false;
    dialog.hidden = false;
    const cancel = $("#delete-cancel");
    if (cancel) cancel.focus();
  }

  function closeDeleteDialog() {
    const dialog = $("#delete-dialog");
    if (!dialog) return;
    dialog.hidden = true;
    $("#delete-backdrop").hidden = true;
    if (deleteTrigger && deleteTrigger.focus) deleteTrigger.focus();
  }

  /* ---------- 就地刷新（片段替换，不丢筛选/排序/分页） ---------- */
  function replaceList(redirect) {
    return fetch(redirect, { headers: { "X-Partial": "list" } })
      .then(function (r) {
        if (!r.ok) throw new Error("列表刷新失败");
        return r.text();
      })
      .then(function (html) {
        const region = $("#list-region");
        if (!region) {
          window.location = redirect;
          return;
        }
        region.innerHTML = html;
        bindRegion();
        window.history.replaceState({}, "", redirect);
        const toast = $(".toast", region);
        if (toast) {
          window.setTimeout(function () {
            toast.hidden = true;
          }, 4000);
        }
      });
  }

  function submitDrawer(form) {
    const body = new FormData(form);
    return fetch(form.getAttribute("action"), {
      method: "POST",
      body: body,
      headers: { Accept: "application/json" },
    }).then(function (r) {
      const type = r.headers.get("Content-Type") || "";
      if (type.indexOf("application/json") < 0) {
        window.location = form.getAttribute("action");
        return null;
      }
      return r.json().then(function (data) {
        if (!r.ok || !data.ok) {
          const box = $(".error-text") || document.createElement("p");
          box.className = "error-text";
          box.setAttribute("role", "alert");
          box.textContent = data.message || "保存失败，请检查填写内容";
          const head = $(".drawer-head");
          if (head && !$(".error-text")) head.parentNode.insertBefore(box, head.nextSibling);
          return null;
        }
        const redirect = data.redirect || "/ui/data-domains";
        history.replaceState({}, "", redirect);
        return replaceList(redirect).then(function () {
          const drawer = $("#drawer");
          const backdrop = $("#drawer-backdrop");
          if (drawer) drawer.remove();
          if (backdrop) backdrop.remove();
        });
      });
    });
  }

  /* ---------- 事件绑定 ---------- */
  function bindRegion() {
    const region = $("#list-region");
    if (!region) return;

    region.querySelectorAll("a[href]").forEach(function (a) {
      if (a.getAttribute("href").indexOf("drawer=") >= 0) {
        a.addEventListener("click", function () {
          rememberTrigger(a);
        });
      }
    });

    region.querySelectorAll("[data-delete-url]").forEach(function (btn) {
      btn.addEventListener("click", function () {
        openDeleteDialog(btn.getAttribute("data-delete-url"), btn.getAttribute("data-delete-name"), btn);
      });
    });

    const q = $("#q", region);
    if (q) {
      let timer = null;
      q.addEventListener("input", function () {
        window.clearTimeout(timer);
        timer = window.setTimeout(function () {
          const form = $("#filter-form");
          if (form) form.submit();
        }, 400);
      });
    }
  }

  function bindPageSize() {
    document.addEventListener("change", function (ev) {
      const el = ev.target;
      if (el && el.matches("select[data-nav]")) {
        window.location = el.value;
      }
    });
  }

  function bindScrollRestore() {
    const key = SCROLL_PREFIX + window.location.pathname + window.location.search;
    const saved = sessionStorage.getItem(key);
    if (saved) window.scrollTo(0, parseInt(saved, 10));
    window.addEventListener("beforeunload", function () {
      sessionStorage.setItem(key, String(window.scrollY || 0));
    });
  }

  function bindGlobal() {
    document.addEventListener("keydown", function (ev) {
      if (ev.key !== "Escape") return;
      const dialog = $("#delete-dialog");
      if (dialog && !dialog.hidden) {
        closeDeleteDialog();
        return;
      }
      if ($("#drawer")) closeDrawer();
    });

    const newBtn = $("#btn-new");
    if (newBtn) {
      newBtn.addEventListener("click", function () {
        rememberTrigger(newBtn);
      });
    }

    const drawerForm = $("#drawer-form");
    if (drawerForm) {
      drawerForm.addEventListener("submit", function (ev) {
        ev.preventDefault();
        submitDrawer(drawerForm);
      });
      const drawer = $("#drawer");
      if (drawer) drawer.focus();
    }

    const backdrop = $("#drawer-backdrop");
    if (backdrop) backdrop.addEventListener("click", closeDrawer);

    const deleteBackdrop = $("#delete-backdrop");
    if (deleteBackdrop) deleteBackdrop.addEventListener("click", closeDeleteDialog);
    const deleteCancel = $("#delete-cancel");
    if (deleteCancel) deleteCancel.addEventListener("click", closeDeleteDialog);

    const focusTarget = document.querySelector("[data-focus-return]");
    if (focusTarget) focusTarget.focus();
  }

  document.addEventListener("DOMContentLoaded", function () {
    bindRegion();
    bindPageSize();
    bindScrollRestore();
    bindGlobal();
  });
})();

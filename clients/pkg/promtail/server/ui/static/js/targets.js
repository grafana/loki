function toggleJobTable(button, shouldExpand){
  if (button.length === 0) { return; }

  if (shouldExpand) {
    button.removeClass("collapsed-table").addClass("expanded-table").html("show less");
  } else {
    button.removeClass("expanded-table").addClass("collapsed-table").html("show more");
  }

  button.parents(".table-container").find("table").toggle(shouldExpand);
  button.parents(".table-container").find(".collapsed-element").toggle(shouldExpand);
}

function showAll(_, container) {
  $(container).show();
}

function showUnready(_, container) {
  const isReady = $(container).find("h2").attr("class").indexOf("danger") < 0;
  if (isReady) { $(container).hide(); }
}

function init() {
  if (!localStorage.selectedTab || localStorage.selectedTab == "all-targets"){
    $("#all-targets").parent().addClass("active");
    $(".table-container").each(showAll);
  } else if (localStorage.selectedTab == "unready-targets") {
    $("#unready-targets").parent().addClass("active");
    $(".table-container").each(showUnready);
  }

  $("button.targets").click(function () {
    const tableTitle = $(this).closest("h2").find("a").attr("id");

    if ($(this).hasClass("collapsed-table")) {
      localStorage.setItem(tableTitle, "expanded");
      toggleJobTable($(this), true);
    } else if ($(this).hasClass("expanded-table")) {
      localStorage.setItem(tableTitle, "collapsed");
      toggleJobTable($(this), false);
    }
  });

  $(".job_header a").each(function (_, link) {
    const cachedTableState = localStorage.getItem($(link).attr("id"));
    if (cachedTableState === "collapsed") {
      toggleJobTable($(this).siblings("button"), false);
    }
  });

  $("#showTargets :input").change(function() {
    const target = $(this).attr("id");

    if (target === "all-targets") {
      $(".table-container").each(showAll);
      localStorage.setItem("selectedTab", "all-targets");
    } else if (target === "unready-targets") {
      $(".table-container").each(showUnready);
      localStorage.setItem("selectedTab", "unready-targets");
    }
  });
}

$(init);

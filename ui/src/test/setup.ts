import "@testing-library/jest-dom/vitest";

// jsdom doesn't implement scrollIntoView; ChatView uses it heavily.
if (typeof Element !== "undefined" && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function () {};
}

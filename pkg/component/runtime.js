/*
 * song Web Components runtime.
 *
 * Baseline, dependency-free JavaScript (an ES module) that implements MDN Web
 * Components on top of the declarative definitions song injects into served
 * HTML documents:
 *
 *   1. Every <template data-song-component="tag-name"> is read.
 *   2. A custom element named tag-name is registered with customElements.define.
 *   3. When an instance is connected, the template content is cloned into the
 *      element's shadow root (open), so CSS is encapsulated and <slot>
 *      projection works exactly as MDN describes.
 *
 * Already-defined elements and documents without definitions are left alone.
 * No frameworks, no CDNs, no build step.
 */
(function () {
	"use strict";

	// defineFromTemplate registers the custom element described by a single
	// <template data-song-component> element.
	function defineFromTemplate(tpl) {
		var name = tpl.getAttribute("data-song-component");
		if (!name || customElements.get(name)) {
			return;
		}
		customElements.define(
			name,
			class extends HTMLElement {
				constructor() {
					super();
					var mode = tpl.getAttribute("shadowrootmode") || "open";
					var root = this.attachShadow({ mode: mode });
					root.appendChild(tpl.content.cloneNode(true));
				}
			}
		);
	}

	// defineAll consumes every declarative component definition in the page.
	function defineAll() {
		var templates = document.querySelectorAll("template[data-song-component]");
		for (var i = 0; i < templates.length; i++) {
			defineFromTemplate(templates[i]);
		}
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", defineAll);
	} else {
		defineAll();
	}
})();

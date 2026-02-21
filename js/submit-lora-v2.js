/**
 * @param {Form} urlForm
 * @param {Form} versionForm
 */
function initSubmitUrlForm(urlForm, versionForm) {
	const fetchModelId = async () => {
		const formData = new FormData(urlForm);
		const civitaiUrl = formData.get("url");
		const modelId = (() => {
			const url = new URL(civitaiUrl);
			const pathname = url.pathname;
			if (!pathname.startsWith("/models/")) return null;
			const modelIdStr0 = pathname.substring(8);
			const modelIdStr1 = (() => {
				const index = modelIdStr0.indexOf("/");
				if (index === -1) return modelIdStr0;
				else return modelIdStr0.substring(0, index);
			})();
			return parseInt(modelIdStr1);
		})();
		const apiUrl = new URL(`/api/v2/civitai/model/${modelId}/version`, location.origin);
		const resp = await fetch(apiUrl);
		if (!resp.ok) {
			alert("failed to get model version data");
			return;
		}

		/**
		 * @type {{ promptList: String[][] }}
		 */
		const { versionIds, versionNames, promptList } = await resp.json();
		const modelIdInput = versionForm.querySelector("#modelid-input");
		modelIdInput.value = modelId;
		for (const [versionIndex, versionId] of versionIds.entries()) {
			const versionClass = `version-${versionIndex}`;

			const versionDiv = document.createElement("div");
			versionDiv.className = "version";

			const versionCheckDiv = document.createElement("div");
			versionCheckDiv.className = "version-check";

			const inputVersionCheck = document.createElement("input");
			inputVersionCheck.name = `check-${versionIndex}`;
			inputVersionCheck.type = "checkbox";
			inputVersionCheck.checked = true;
			inputVersionCheck.addEventListener("click", () => {
				const targets = versionForm.querySelectorAll(`.${versionClass}`);
				for (const target of targets) {
					target.disabled = !inputVersionCheck.checked;
				}
			});

			const pVersionName = document.createElement("p");
			pVersionName.textContent = versionNames[versionIndex];

			const inputVersionId = document.createElement("input");
			inputVersionId.name = `version-${versionIndex}`;
			inputVersionId.type = "text";
			inputVersionId.value = versionId;
			inputVersionId.readOnly = true;
			inputVersionId.className = `${versionClass} model-id-input`;

			versionCheckDiv.appendChild(inputVersionCheck);
			versionCheckDiv.appendChild(pVersionName);
			versionCheckDiv.appendChild(inputVersionId);
			versionDiv.appendChild(versionCheckDiv);

			const promptListDiv = document.createElement("div");
			promptListDiv.className = "promptlist";

			/**
			 * @param {HTMLDivElement} promptListDiv
			 */
			function addPromptListItem(promptListDiv) {
				const itemNum = promptListDiv.querySelectorAll("div.promptlist-item").length;

				const itemDiv = document.createElement("div");
				itemDiv.className = "promptlist-item";
				const promptsDiv = document.createElement("div");
				promptsDiv.className = "prompts";

				/**
				 * @param {String} prompt
				 */
				function addPrompt(prompt) {
					const promptNum = promptsDiv.querySelectorAll("div.prompt").length;

					const promptDiv = document.createElement("div");
					promptDiv.className = "prompt";

					const promptInput = document.createElement("input");
					promptInput.name = `prompt-${versionIndex}-${itemNum}-${promptNum}`;
					promptInput.type = "text";
					promptInput.required = true;
					promptInput.value = prompt;
					promptInput.className = versionClass;

					promptDiv.appendChild(promptInput);
					promptsDiv.appendChild(promptDiv);
				}
				const addButton = document.createElement("button");
				addButton.textContent = "Add Prompt";
				addButton.type = "button";
				addButton.addEventListener("click", () => {
					addPrompt("");
				});
				addButton.className = versionClass;

				const removeButton = document.createElement("button");
				removeButton.textContent = "Remove Prompt";
				removeButton.type = "button";
				removeButton.addEventListener("click", () => {
					const items = promptsDiv.querySelectorAll(".prompt");
					if (items.length === 0) return;
					items[items.length - 1].remove();
				})
				removeButton.className = versionClass;

				itemDiv.appendChild(promptsDiv);
				itemDiv.appendChild(addButton);
				itemDiv.appendChild(removeButton);
				promptListDiv.appendChild(itemDiv);

				return addPrompt;
			}

			const addPrompt = addPromptListItem(promptListDiv);
			for (const prompt of promptList[versionIndex].values())
				addPrompt(prompt);

			const addButton = document.createElement("button");
			addButton.textContent = "Add List";
			addButton.type = "button";
			addButton.addEventListener("click", () => {
				addPromptListItem(promptListDiv);
			});
			addButton.className = versionClass;

			const removeButton = document.createElement("button");
			removeButton.textContent = "Remove List";
			removeButton.type = "button";
			removeButton.addEventListener("click", () => {
				const items = promptListDiv.querySelectorAll(".promptlist-item");
				if (items.length === 0) return
				items[items.length - 1].remove();
			})
			removeButton.className = versionClass;

			versionDiv.appendChild(promptListDiv);
			versionDiv.appendChild(addButton);
			versionDiv.appendChild(removeButton);
			versionForm.appendChild(versionDiv);
		}

		const submit = document.createElement("input");
		submit.type = "submit";
		submit.value = "Submit";
		submit.id = "submit";
		versionForm.appendChild(submit);
	};
	urlForm.onsubmit = () => {
		fetchModelId();
		return false;
	};
	versionForm.onsubmit = () => {
		const submit = document.querySelector("#submit")
		submit.disabled = true;
		const formData = new FormData(versionForm);
		const formDataC = new FormData();
		const modelId = parseInt(formData.get("modelId"));

		/** @type {Map<number, number>} */
		const versionIds = new Map();
		/** @type {Map<number, string[][]} */
		const prompts = new Map();

		for (const [key, value] of formData.entries()) {
			if (key.startsWith("version-")) {
				const versionIndex = parseInt(key.replace("version-", ""));
				versionIds.set(versionIndex, parseInt(value));
			}
			else if (key.startsWith("prompt-")) {
				const key0 = key.replace("prompt-", "");
				const sep0 = key0.indexOf("-");
				const versionIndex = parseInt(key0.substring(0, sep0))
				const sep1 = key0.indexOf("-", sep0 + 1);
				const promptListIndex = parseInt(key0.substring(sep0 + 1, sep1));
				const promptIndex = parseInt(key0.substring(sep1 + 1));

				const ps = prompts.get(versionIndex) ?? [];
				ps[promptListIndex] ??= [];
				ps[promptListIndex][promptIndex] = value;
				prompts.set(versionIndex, ps);
			}
		}

		/** @type {number[]} */
		const versionIdArr = [];
		/** @type {string[][][]} */
		const promptArr = [];
		for (const key of Array.from(versionIds.keys()).sort((a, b) => a - b).values()) {
			versionIdArr.push(versionIds.get(key));
			promptArr.push(prompts.get(key) ?? [[]]);
		}

		formDataC.set("prompts", JSON.stringify(promptArr));
		formDataC.set("versionIds", JSON.stringify(versionIdArr));

		const xhr = new XMLHttpRequest();
		xhr.open("POST", `/api/v2/lora/${modelId}`);
		xhr.send(formDataC);
		xhr.onreadystatechange = () => {
			if (xhr.readyState == XMLHttpRequest.DONE) {
				if (xhr.status >= 200 && xhr.status < 300) {
					urlForm.reset();
					versionForm.reset();
					alert("Submitted");
					location.reload();
				} else {
					alert("Failed to submit form");
				}
			}
		}
		return false;
	};
}

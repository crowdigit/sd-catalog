/**
 * @param {Form} urlForm
 * @param {Form} versionForm
 */
function initSubmitUrlForm(versionForm) {
	const fetchModelId = async () => {
		const modelId = 1;
		/**
		 * @type {{ promptList: String[][] }}
		 */
		const { versionIds, versionNames, promptList } = { versionIds: [1], versionNames: ["version name"], promptList: [[]] };
		const modelIdInput = versionForm.querySelector("#modelid-input");
		modelIdInput.value = modelId;
		for (const [versionIndex, versionId] of versionIds.entries()) {
			const versionClass = `version-${versionIndex}`;

			const versionDiv = document.createElement("div");
			versionDiv.className = "version";

			const versionCheckDiv = document.createElement("div");
			versionCheckDiv.className = "version-check";

			const labelVersionName = document.createElement("label");
			labelVersionName.textContent = "Version Name"
			labelVersionName.htmlFor = `versionName`;

			const inputVersionName = document.createElement("input");
			inputVersionName.name = `versionName`;
			inputVersionName.type = "text";
			inputVersionName.className = `${versionClass}`;
			inputVersionName.required = true;

			const labelVersionId = document.createElement("label");
			labelVersionId.textContent = "Version ID"
			labelVersionId.htmlFor = `version-${versionIndex}`;

			const inputVersionId = document.createElement("input");
			inputVersionId.name = `version-${versionIndex}`;
			inputVersionId.type = "text";
			inputVersionId.value = versionId;
			inputVersionId.className = `${versionClass} model-id-input`;
			inputVersionId.required = true;

			const labelFilename = document.createElement("label");
			labelFilename.textContent = "Filename"
			labelFilename.htmlFor = `filename`;

			const inputFilename = document.createElement("input");
			inputFilename.name = `filename`;
			inputFilename.type = "text";
			inputFilename.pattern = ".+\.safetensors";
			inputFilename.className = `${versionClass}`;
			inputFilename.required = true;

			versionCheckDiv.appendChild(labelVersionName);
			versionCheckDiv.appendChild(inputVersionName);
			versionCheckDiv.appendChild(document.createElement("br"));
			versionCheckDiv.appendChild(labelVersionId);
			versionCheckDiv.appendChild(inputVersionId);
			versionCheckDiv.appendChild(document.createElement("br"));
			versionCheckDiv.appendChild(labelFilename);
			versionCheckDiv.appendChild(inputFilename);

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
	versionForm.onsubmit = () => {
		const submit = document.querySelector("#submit")
		submit.disabled = true;
		const formData = new FormData(versionForm);
		const formDataC = new FormData();
		const modelId = parseInt(formData.get("modelId"));
		const modelName = formData.get("modelName");
		const versionName = formData.get("versionName");
		const filename = formData.get("filename");

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

		const url = new URL(`/api/v2/lora/${modelId}`,  location.origin);
		url.searchParams.set("manual", "true");
		url.searchParams.set("modelName", modelName);
		url.searchParams.set("versionName", versionName);
		url.searchParams.set("filename", filename);

		const xhr = new XMLHttpRequest();
		xhr.open("POST", url.href);
		xhr.send(formDataC);
		xhr.onreadystatechange = () => {
			if (xhr.readyState == XMLHttpRequest.DONE) {
				if (xhr.status >= 200 && xhr.status < 300) {
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
	fetchModelId();
}

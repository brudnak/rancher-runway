const copyWithLegacySelection = text => {
  if (!document.body || typeof document.execCommand !== "function") {
    return false;
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.inset = "0 auto auto -9999px";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();

  try {
    return document.execCommand("copy");
  } finally {
    textarea.remove();
  }
};

export const writeTextToClipboard = async value => {
  const text = String(value ?? "");
  if (!text.trim()) {
    throw new Error("No value is available to copy yet.");
  }

  // The control panel is served as the top-level Wails document. Its native
  // runtime clipboard call avoids WebKit's permission-gated Clipboard API.
  const nativeCopy = window.runtime?.ClipboardSetText;
  if (typeof nativeCopy === "function") {
    const copied = await nativeCopy(text);
    if (copied === false) {
      throw new Error("The desktop clipboard rejected the copy request.");
    }
    return;
  }

  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch (error) {
      if (copyWithLegacySelection(text)) {
        return;
      }
      throw error;
    }
  }

  if (!copyWithLegacySelection(text)) {
    throw new Error("Clipboard access is unavailable in this browser.");
  }
};

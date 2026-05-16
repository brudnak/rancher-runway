export const createTypedConfirmation = ({
  modalEl,
  accentEl,
  titleEl,
  bodyEl,
  promptEl,
  inputEl,
  errorEl,
  cancelEl,
  submitEl
}) => ({ title, body, typedValue, confirmText, accentText = 'Confirmation required' }) => new Promise(resolve => {
  if (!modalEl) {
    resolve(false)
    return
  }

  let settled = false
  const expected = String(typedValue || '').trim().toLowerCase()

  const cleanup = result => {
    if (settled) {
      return
    }
    settled = true
    modalEl.classList.add('hidden')
    modalEl.classList.remove('flex')
    document.body.classList.remove('overflow-hidden')
    cancelEl.removeEventListener('click', cancel)
    submitEl.removeEventListener('click', submit)
    inputEl.removeEventListener('keydown', keydown)
    modalEl.removeEventListener('click', backdrop)
    document.removeEventListener('keydown', escape)
    resolve(result)
  }

  const cancel = () => cleanup(false)
  const submit = () => {
    if (String(inputEl.value || '').trim().toLowerCase() !== expected) {
      errorEl.textContent = `Type ${typedValue} to confirm.`
      inputEl.focus()
      inputEl.select()
      return
    }
    cleanup(true)
  }
  const keydown = event => {
    if (event.key === 'Enter') {
      event.preventDefault()
      submit()
    }
  }
  const backdrop = event => {
    if (event.target === modalEl) {
      cancel()
    }
  }
  const escape = event => {
    if (event.key === 'Escape') {
      cancel()
    }
  }

  accentEl.textContent = accentText
  titleEl.textContent = title
  bodyEl.textContent = body
  promptEl.textContent = `Type "${typedValue}" to continue`
  submitEl.textContent = confirmText
  inputEl.value = ''
  errorEl.textContent = ''
  modalEl.classList.remove('hidden')
  modalEl.classList.add('flex')
  document.body.classList.add('overflow-hidden')
  cancelEl.addEventListener('click', cancel)
  submitEl.addEventListener('click', submit)
  inputEl.addEventListener('keydown', keydown)
  modalEl.addEventListener('click', backdrop)
  document.addEventListener('keydown', escape)
  window.setTimeout(() => inputEl.focus(), 0)
})

export const createNoticeController = ({ noticeEl, titleEl, bodyEl, closeEl, fallback }) => {
  let timer = null

  const hide = () => {
    if (timer) {
      window.clearTimeout(timer)
      timer = null
    }
    noticeEl?.classList.add('hidden')
  }

  const show = (title, body) => {
    if (!noticeEl) {
      fallback?.(title, body)
      return
    }
    if (titleEl) {
      titleEl.textContent = title
    }
    if (bodyEl) {
      bodyEl.textContent = body
    }
    noticeEl.classList.remove('hidden')
    if (timer) {
      window.clearTimeout(timer)
    }
    timer = window.setTimeout(() => {
      noticeEl.classList.add('hidden')
      timer = null
    }, 9000)
  }

  closeEl?.addEventListener('click', hide)
  return { hide, show }
}

export const createBasicModal = ({ modalEl, closeEl, unavailable }) => {
  const close = () => {
    modalEl?.classList.add('hidden')
    modalEl?.classList.remove('flex')
    document.body.classList.remove('overflow-hidden')
  }

  const show = () => {
    if (!modalEl) {
      unavailable?.()
      return
    }
    modalEl.classList.remove('hidden')
    modalEl.classList.add('flex')
    document.body.classList.add('overflow-hidden')
    window.setTimeout(() => closeEl?.focus(), 0)
  }

  closeEl?.addEventListener('click', close)
  return { close, show }
}

(() => {
  const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  const reveals = document.querySelectorAll('.reveal-on-scroll');
  if (reduceMotion || !('IntersectionObserver' in window)) {
    reveals.forEach((element) => element.classList.add('is-visible'));
  } else {
    reveals.forEach((element) => element.classList.add('reveal-ready'));
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        entry.target.classList.add('is-visible');
        observer.unobserve(entry.target);
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -40px' });
    reveals.forEach((element) => observer.observe(element));
  }

  const stageContent = [
    {
      label: 'STAGE 01 · BEFORE SOURCE REVIEW',
      title: 'Build the hypotheses before reading the implementation.',
      body: 'Claims, manifests, and exposed capabilities become a target-specific threat model across twelve AI-native risk classes.'
    },
    {
      label: 'STAGE 02 · PARALLEL INVESTIGATION',
      title: 'Give every credible threat its own line of inquiry.',
      body: 'Independent investigators trace concrete sources, sinks, permissions, and trust boundaries without losing context to unrelated hypotheses.'
    },
    {
      label: 'STAGE 03 · EVIDENCE VALIDATION',
      title: 'Make the model prove what it claims.',
      body: 'Assay re-opens every cited file and checks the quoted source near the claimed line. Unsupported evidence—and findings left without evidence—are dropped.'
    },
    {
      label: 'STAGE 04 · DETERMINISTIC SYNTHESIS',
      title: 'Turn verified evidence into a reproducible verdict.',
      body: 'Severity arithmetic and a deterministic security floor compute safe, caution, or unsafe. The model never gets to improvise the final outcome.'
    }
  ];

  const tabs = [...document.querySelectorAll('.method-tab')];
  const panel = document.querySelector('.stage-panel');
  const label = panel?.querySelector('.stage-label');
  const title = panel?.querySelector('h3');
  const body = panel?.querySelector('p');

  tabs.forEach((tab, index) => {
    tab.addEventListener('click', () => {
      tabs.forEach((item) => {
        const active = item === tab;
        item.classList.toggle('active', active);
        item.setAttribute('aria-selected', String(active));
      });
      if (!panel || !label || !title || !body) return;
      panel.classList.remove('stage-changing');
      void panel.offsetWidth;
      label.textContent = stageContent[index].label;
      title.textContent = stageContent[index].title;
      body.textContent = stageContent[index].body;
      panel.classList.add('stage-changing');
    });
    tab.addEventListener('keydown', (event) => {
      if (!['ArrowDown', 'ArrowRight', 'ArrowUp', 'ArrowLeft'].includes(event.key)) return;
      event.preventDefault();
      const direction = ['ArrowDown', 'ArrowRight'].includes(event.key) ? 1 : -1;
      const next = tabs[(index + direction + tabs.length) % tabs.length];
      next.focus();
      next.click();
    });
  });

  const tilt = document.querySelector('[data-tilt]');
  if (tilt && !reduceMotion && window.matchMedia('(pointer: fine)').matches) {
    tilt.addEventListener('pointermove', (event) => {
      const bounds = tilt.getBoundingClientRect();
      const x = (event.clientX - bounds.left) / bounds.width - 0.5;
      const y = (event.clientY - bounds.top) / bounds.height - 0.5;
      tilt.style.setProperty('--tilt-x', `${(-y * 2.2).toFixed(2)}deg`);
      tilt.style.setProperty('--tilt-y', `${(x * 2.2).toFixed(2)}deg`);
    });
    tilt.addEventListener('pointerleave', () => {
      tilt.style.setProperty('--tilt-x', '0deg');
      tilt.style.setProperty('--tilt-y', '0deg');
    });
  }

  const copyButton = document.querySelector('[data-copy]');
  const installCode = document.querySelector('#install-code');
  copyButton?.addEventListener('click', async () => {
    if (!installCode) return;
    const original = copyButton.querySelector('span')?.textContent ?? 'Copy';
    try {
      await navigator.clipboard.writeText(installCode.textContent ?? '');
      const text = copyButton.querySelector('span');
      if (text) text.textContent = 'Copied';
      setTimeout(() => { if (text) text.textContent = original; }, 1600);
    } catch {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(installCode);
      selection?.removeAllRanges();
      selection?.addRange(range);
    }
  });
})();

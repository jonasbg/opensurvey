// emoji.js

function generateUniqueId() {
  return Math.random().toString(36).substr(2, 9);
}

export function createFloatingEmoji(emoji, providedId = null) {
  const emojiId = providedId || `${emoji};${generateUniqueId()}`;

  // Check if the emoji already exists in the DOM
  if (document.querySelector(`[data-emoji-id="${emojiId}"]`)) {
    console.log(`Emoji with ID ${emojiId} already exists. Skipping creation.`);
    return null;
  }

  const emojiElement = document.createElement('div');
  emojiElement.textContent = emoji;
  emojiElement.setAttribute('data-emoji-id', emojiId);
  emojiElement.classList.add('floating-emoji');

  // Set random starting position
  const startX = Math.random() * (window.innerWidth - 50);
  emojiElement.style.left = `${startX}px`;

  // Set random animation duration and delay
  const duration = 7 + Math.random() * 6;
  const delay = Math.random() * 2;
  emojiElement.style.animationDuration = `${duration}s, 3s`;
  emojiElement.style.animationDelay = `${delay}s, ${delay}s`;

  document.body.appendChild(emojiElement);

  // Remove the emoji after the animation completes
  setTimeout(() => {
    emojiElement.remove();
  }, (duration + delay) * 1000);

  // Explosion effect
  emojiElement.addEventListener('click', (e) => {
    e.stopPropagation();
    explodeEmoji(emojiElement);
    if (window.sendEmojiPoppedMessage) {
      window.sendEmojiPoppedMessage(emojiId);
    }
  });

  // Touch support for mobile devices
  emojiElement.addEventListener('touchstart', (e) => {
    e.preventDefault();
    explodeEmoji(emojiElement);
    if (window.sendEmojiPoppedMessage) {
      window.sendEmojiPoppedMessage(emojiId);
    }
  });

  return emojiId;
}

export function explodeEmoji(emojiElement) {
  const explosionPieces = 8;
  const rect = emojiElement.getBoundingClientRect();
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;

  for (let i = 0; i < explosionPieces; i++) {
    const piece = document.createElement('div');
    piece.textContent = emojiElement.textContent;
    piece.style.position = 'fixed';
    piece.style.fontSize = '1em';
    piece.style.left = centerX + 'px';
    piece.style.top = centerY + 'px';
    piece.style.zIndex = 1001;
    document.body.appendChild(piece);

    const angle = (i / explosionPieces) * 2 * Math.PI;
    const velocity = 5 + Math.random() * 5;
    const dx = Math.cos(angle) * velocity;
    const dy = Math.sin(angle) * velocity;

    piece.animate([
      { transform: 'translate(0, 0) scale(1)', opacity: 1 },
      { transform: `translate(${dx * 20}px, ${dy * 20}px) scale(0)`, opacity: 0 }
    ], {
      duration: 1000,
      easing: 'ease-out'
    }).onfinish = () => piece.remove();
  }

  emojiElement.remove();
}
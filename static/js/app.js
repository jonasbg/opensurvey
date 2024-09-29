import { createFloatingEmoji, explodeEmoji } from './emoji.js';

(function () {
    let socket;
    const token = getTokenFromUrl();


    // Global state object
    window.appState = {
        results: {},
        userCount: 0,
        subscribers: [],
        enableEmojis: false,

        // Method to update state and notify subscribers
        setState: function (key, value) {
            this[key] = value;
            this.notifySubscribers(key, value);
        },

        // Subscribe to state changes
        subscribe: function (callback) {
            this.subscribers.push(callback);
        },

        // Notify all subscribers of a state change
        notifySubscribers: function (key, value) {
            this.subscribers.forEach(callback => callback(key, value));
        }
    };

    window.sendEmoji = function(emoji) {
        if (socket && socket.readyState === WebSocket.OPEN) {
            const emojiId = createFloatingEmoji(emoji);
            if (emojiId) {
                socket.send(JSON.stringify({type: "emoji", payload: emojiId}));
            }
        }
    };


    function getTokenFromUrl() {
        const pathParts = window.location.pathname.split('/');
        return pathParts[pathParts.length - 1];
    }

    function connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        socket = new WebSocket(`${protocol}//${window.location.host}/ws`);
        
        socket.onmessage = function (event) {
            const message = JSON.parse(event.data);
            if (message.type === "newSlide") {
                if (window.location.pathname.indexOf("results") > 0) {
                    // Redirect to the survey
                    window.location.href = `/survey/${token}`;
                } else {
                    // Reload the survey page
                    window.location.reload();
                }
            } else if (message.type === "newAnswer") {
                // Update the results in the appState
                window.appState.setState('results', message.payload);
            } else if (message.type === "userCount") {
                // Update the user count in the appState
                window.appState.setState('userCount', message.payload);
            } else if (message.type === "finished") {
                window.location.href = `/completed/${token}`;
            } else if (message.type === "emoji") {
                if (window.appState.enableEmojis) {
                    const [emoji, id] = message.payload.split(';');
                    createFloatingEmoji(emoji, message.payload);
                }
            } else if (message.type === "emojiPopped") {
                const emojiToRemove = document.querySelector(`[data-emoji-id="${message.payload}"]`);
                if (emojiToRemove) {
                    explodeEmoji(emojiToRemove);
                }
            }
        };

        socket.onclose = function (event) {
            console.log("WebSocket connection closed. Reconnecting...");
            setTimeout(connectWebSocket, 1000);
        };
    }

    connectWebSocket();

    // Function to update results in the DOM when appState changes
    function updateResults(results) {
        const resultsList = document.getElementById('results-list');
        if (!resultsList) return; // Not on the results page

        // Update existing answers or add new ones
        for (const [answer, count] of Object.entries(results)) {
            let li = resultsList.querySelector(`li[data-answer="${answer}"]`);
            if (li) {
                li.querySelector('.count').textContent = count;
            } else {
                li = document.createElement('li');
                li.setAttribute('data-answer', answer);
                li.innerHTML = `${answer}: <span class="count">${count}</span>`;
                resultsList.appendChild(li);
            }
        }

        // Remove answers that are no longer present
        resultsList.querySelectorAll('li').forEach(li => {
            const answer = li.getAttribute('data-answer');
            if (!(answer in results)) {
                li.remove();
            }
        });
    }

    // Function to update the user count in the DOM when appState changes
    function updateUserCount(count) {
        const userCountElement = document.getElementById('user-count');
        if (userCountElement) {
            userCountElement.textContent = count;
        }
    }

    // Subscribe to state changes
    window.appState.subscribe((key, value) => {
        if (key === 'results') {
            updateResults(value);
        } else if (key === 'userCount') {
            updateUserCount(value);
        }
    });

    // For the presenter: Function to move to the next slide
    window.nextSlide = function () {
        fetch('/nextSlide', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/x-www-form-urlencoded',
            },
            body: `secret=${prompt("Enter presenter secret:")}`
        }).then(response => {
            if (!response.ok) {
                alert("Failed to move to the next slide. Check your secret.");
            }
        });
    };

    // Emoji button event listener
    const emojiButton = document.getElementById('emoji-button');
    if (emojiButton) {
        emojiButton.addEventListener('click', function () {
            const emoji = ['😀', '😍', '🎉', '👍', '🚀'][Math.floor(Math.random() * 5)];
            sendEmoji(emoji);
        });
    }

    function rewriteURL(token) {
        if (token && (window.location.pathname !== '/survey/') + token) {
            window.history.replaceState(null, '', '/survey/' + token);
        }
    }

    document.addEventListener('DOMContentLoaded', function () {
        rewriteURL(token);
    });

})();

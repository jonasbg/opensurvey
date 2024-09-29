function updateBarChart() {
  const container = document.getElementById('chart-container');
  if (!container) return;

  const results = window.appState.results;
  const maxValue = Math.max(...Object.values(results));

  Object.entries(results).forEach(([answer, count]) => {
      let bar = container.querySelector(`.bar[data-answer="${answer}"]`);
      const width = (count / maxValue) * 100;

      if (bar) {
          const value = bar.querySelector('.bar-value');
          value.style.width = `${width}%`;
          value.classList.add('updated');
          setTimeout(() => value.classList.remove('updated'), 500);
      } else {
          bar = document.createElement('div');
          bar.className = 'bar';
          bar.setAttribute('data-answer', answer);

          const label = document.createElement('div');
          label.className = 'bar-label';
          label.textContent = answer;

          const value = document.createElement('div');
          value.className = 'bar-value';
          value.style.width = `${width}%`;

          bar.appendChild(label);
          bar.appendChild(value);
          container.appendChild(bar);
      }
  });

  // Remove bars for answers that no longer exist
  container.querySelectorAll('.bar').forEach(bar => {
      const answer = bar.getAttribute('data-answer');
      if (!(answer in results)) {
          bar.remove();
      }
  });
}

function getColor(index) {
  const colors = ['#FF6384', '#36A2EB', '#FFCE56', '#4BC0C0', '#9966FF', '#FF9F40'];
  return colors[index % colors.length];
}

window.appState.subscribe((key, value) => {
  if (key === 'results') {
      updateBarChart();
  }
});

addEventListener("DOMContentLoaded", (event) => {
  window.appState.enableEmojis = true;
  const resultsList = document.getElementById('results-list');
  if (resultsList) {
      const initialResults = {};
      const listItems = resultsList.querySelectorAll('li');
      listItems.forEach(li => {
          const answer = li.getAttribute('data-answer');
          const count = parseInt(li.querySelector('.count').textContent);
          initialResults[answer] = count;
      });

      window.appState.results = initialResults;

      updateBarChart();
  }
});

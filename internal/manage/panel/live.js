// One connection per signed-in page. Generation guards cover both EventSource
// callbacks and session checks that finish after logout, hiding or reconnecting.
export function overviewStream({
  request,
  snapshot,
  unauthorized,
  disconnected,
}) {
  let source,
    timer,
    generation = 0,
    active = false,
    delay = 1000;
  function close() {
    generation++;
    clearTimeout(timer);
    timer = null;
    source?.close();
    source = null;
  }
  function connect() {
    if (!active || source) return;
    const current = generation;
    const connection = new EventSource("/api/events");
    source = connection;
    connection.addEventListener("overview", (event) => {
      if (current !== generation || source !== connection) return;
      try {
        snapshot(JSON.parse(event.data));
        delay = 1000;
      } catch {
        retry();
      }
    });
    connection.onerror = retry;
    function retry() {
      if (current !== generation || source !== connection) return;
      close();
      const attempt = generation;
      const wait = delay + Math.floor(Math.random() * 500);
      delay = Math.min(delay * 2, 30000);
      disconnected?.();
      timer = setTimeout(async () => {
        timer = null;
        if (!active || generation !== attempt) return;
        try {
          await request("/api/auth/session");
        } catch (error) {
          if (generation !== attempt) return;
          if (error.status === 401) {
            active = false;
            close();
            unauthorized();
            return;
          }
        }
        if (active && generation === attempt) connect();
      }, wait);
    }
  }
  return {
    setActive(value) {
      active = value;
      if (!active) close();
      else if (!source && !timer) connect();
    },
    stop() {
      active = false;
      close();
      delay = 1000;
    },
  };
}

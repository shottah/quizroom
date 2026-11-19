import { isDown } from "./api/client";
import { ChatBubble } from "./components/ChatBubble";
import { useStatus } from "./hooks";
import { Home } from "./pages/Home";
import { QuizPage } from "./pages/QuizPage";
import { SettingsPage } from "./pages/SettingsPage";
import { TopicPage } from "./pages/TopicPage";
import { useRoute } from "./router";

export function App() {
  const route = useRoute();
  const status = useStatus();
  const down = isDown(status.error);
  return (
    <>
      {down ? (
        <div className="global-banner" role="alert">
          Can't reach Quizroom. Run <code>make up</code> in your Quizroom folder, then{" "}
          <button className="link" onClick={() => window.location.reload()}>
            Reload
          </button>
          .
        </div>
      ) : null}
      <nav className="nav">
        <a className="brand" href="#/">
          <span className="logo" aria-hidden="true" />
          Quizroom
        </a>
        <div className="nav-links">
          <a href="#/" className={route.name === "home" ? "active" : ""}>
            Topics
          </a>
          <a href="#/settings" className={route.name === "settings" ? "active" : ""}>
            Settings
          </a>
        </div>
      </nav>
      {route.name === "home" ? <Home /> : null}
      {route.name === "quiz" ? <QuizPage key={route.id} id={route.id} /> : null}
      {route.name === "topic" ? <TopicPage key={route.slug} slug={route.slug} /> : null}
      {route.name === "settings" ? <SettingsPage /> : null}
      <ChatBubble />
    </>
  );
}

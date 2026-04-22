import { Component, type ErrorInfo, type ReactNode } from "react";

type Props = { children: ReactNode };
type State = { error: Error | null };

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ErrorBoundary]", error, info.componentStack);
  }

  override render() {
    if (this.state.error) {
      return (
        <div className="console-root">
          <div className="console-layout" style={{ gridTemplateColumns: "1fr" }}>
            <div className="console-surface" style={{ padding: "2rem", marginTop: "2rem" }}>
              <p className="console-eyebrow">Runtime error</p>
              <h2 className="console-section__title" style={{ marginBottom: "1rem" }}>Something went wrong</h2>
              <p className="body-muted" style={{ marginBottom: "1rem" }}>
                {this.state.error.message || "An unexpected error occurred."}
              </p>
              <button
                className="toolbar-button toolbar-button--primary"
                onClick={() => window.location.reload()}
                type="button"
              >
                Reload
              </button>
            </div>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

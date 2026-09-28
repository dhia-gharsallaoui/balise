import { Component, type ReactNode } from "react";
import { EmptyState } from "./EmptyState";

interface Props {
  // Changing this remounts the boundary's children and clears a caught error, so moving to
  // another section recovers without a reload.
  resetKey: string;
  children: ReactNode;
}

interface State {
  error: Error | null;
  resetKey: string;
}

// Contains a render error to the one screen that threw it. Without this, any exception while
// rendering a section unmounts the whole tree and leaves a blank page with no way back (this
// is how a null `proposals` array once blanked the entire app from Home).
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, resetKey: this.props.resetKey };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    return props.resetKey !== state.resetKey ? { error: null, resetKey: props.resetKey } : null;
  }

  componentDidCatch(error: Error) {
    console.error("Screen failed to render:", error);
  }

  render() {
    if (this.state.error) {
      return (
        <EmptyState
          title="This screen failed to load."
          hint={`${this.state.error.message}. Other sections still work; reload to try this one again.`}
        />
      );
    }
    return this.props.children;
  }
}

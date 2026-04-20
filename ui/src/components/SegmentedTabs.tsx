type SegmentedTab<T extends string> = {
  id: T;
  label: string;
};

type SegmentedTabsProps<T extends string> = {
  tabs: Array<SegmentedTab<T>>;
  value: T;
  onChange: (value: T) => void;
};

export function SegmentedTabs<T extends string>(props: SegmentedTabsProps<T>) {
  return (
    <div className="inline-flex flex-wrap gap-1 rounded-full bg-slate-200/80 p-1">
      {props.tabs.map((tab) => (
        <button
          className={
            props.value === tab.id
              ? "rounded-full bg-white px-3 py-2 text-sm font-medium text-slate-900 shadow"
              : "rounded-full px-3 py-2 text-sm text-slate-600"
          }
          key={tab.id}
          onClick={() => props.onChange(tab.id)}
          type="button"
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

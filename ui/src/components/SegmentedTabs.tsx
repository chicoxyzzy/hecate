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
    <div className="segmented-tabs">
      {props.tabs.map((tab) => (
        <button
          className={props.value === tab.id ? "segmented-tabs__item segmented-tabs__item--active" : "segmented-tabs__item"}
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

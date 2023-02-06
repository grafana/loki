{
  _images+:: {
    loki: 'grafana/loki:2.7.3',

    read: self.loki,
    write: self.loki,
  },
}
